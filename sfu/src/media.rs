//! Media path (webrtc-rs) — `--features media`
//!
//! DOJO SFU:
//! - Per-peer RTCPeerConnection (client offer → SFU answer)
//! - Track fan-out: OnTrack → TrackLocalStaticRTP → other peers
//! - Outbound DataChannels `glyph` / `hex` / `chat` (SFU-created) + remote DCs
//! - Lane fan-out over DC + WS mirror for terminals

#![cfg(feature = "media")]

use std::collections::HashMap;
use std::sync::Arc;

use bytes::Bytes;
use serde_json::json;
use tokio::sync::{mpsc, Mutex};
use tracing::{info, warn};
use uuid::Uuid;
use webrtc::api::interceptor_registry::register_default_interceptors;
use webrtc::api::media_engine::MediaEngine;
use webrtc::api::APIBuilder;
use webrtc::api::API;
use webrtc::data_channel::data_channel_message::DataChannelMessage;
use webrtc::data_channel::RTCDataChannel;
use webrtc::ice_transport::ice_candidate::RTCIceCandidateInit;
use webrtc::ice_transport::ice_server::RTCIceServer;
use webrtc::peer_connection::configuration::RTCConfiguration;
use webrtc::peer_connection::peer_connection_state::RTCPeerConnectionState;
use webrtc::peer_connection::sdp::session_description::RTCSessionDescription;
use webrtc::peer_connection::RTCPeerConnection;
use webrtc::rtp_transceiver::rtp_codec::RTCRtpCodecCapability;
use webrtc::track::track_local::track_local_static_rtp::TrackLocalStaticRTP;
use webrtc::track::track_local::{TrackLocal, TrackLocalWriter};
use webrtc::track::track_remote::TrackRemote;

use crate::signaling::ServerMsg;

const OUTBOUND_DC_LABELS: &[&str] = &["glyph", "hex", "chat"];

/// Shared media engine for all SFU peer connections.
pub struct MediaHub {
    api: Arc<API>,
    config: RTCConfiguration,
    peers: Mutex<HashMap<Uuid, Arc<PeerMedia>>>,
}

struct PeerMedia {
    id: Uuid,
    room: String,
    nick: String,
    pc: Arc<RTCPeerConnection>,
    signal: mpsc::UnboundedSender<ServerMsg>,
    /// Outbound tracks we send *to this peer* (keyed by publisher_id + track id)
    outbound: Mutex<HashMap<String, Arc<TrackLocalStaticRTP>>>,
    /// DataChannels by label (SFU-created outbound + any client-created)
    dcs: Mutex<HashMap<String, Arc<RTCDataChannel>>>,
}

impl MediaHub {
    pub async fn new() -> Result<Arc<Self>, webrtc::Error> {
        let mut m = MediaEngine::default();
        m.register_default_codecs()?;
        let mut registry = webrtc::interceptor::registry::Registry::new();
        registry = register_default_interceptors(registry, &mut m)?;
        let api = APIBuilder::new()
            .with_media_engine(m)
            .with_interceptor_registry(registry)
            .build();

        let mut ice_servers = vec![RTCIceServer {
            urls: vec!["stun:stun.l.google.com:19302".to_owned()],
            ..Default::default()
        }];
        if let Ok(raw) = std::env::var("GY_SFU_TURN_URLS") {
            for part in raw.split(';').filter(|s| !s.is_empty()) {
                let bits: Vec<_> = part.split(',').map(str::trim).collect();
                if bits.is_empty() {
                    continue;
                }
                ice_servers.push(RTCIceServer {
                    urls: vec![bits[0].to_owned()],
                    username: bits.get(1).unwrap_or(&"").to_string(),
                    credential: bits.get(2).unwrap_or(&"").to_string(),
                    ..Default::default()
                });
            }
        }

        Ok(Arc::new(Self {
            api: Arc::new(api),
            config: RTCConfiguration {
                ice_servers,
                ..Default::default()
            },
            peers: Mutex::new(HashMap::new()),
        }))
    }

    pub async fn ensure_peer(
        self: &Arc<Self>,
        peer_id: Uuid,
        room: String,
        nick: String,
        signal: mpsc::UnboundedSender<ServerMsg>,
    ) -> Result<(), String> {
        {
            let guard = self.peers.lock().await;
            if guard.contains_key(&peer_id) {
                return Ok(());
            }
        }

        let pc = self
            .api
            .new_peer_connection(self.config.clone())
            .await
            .map_err(|e| e.to_string())?;
        let pc = Arc::new(pc);

        let session = Arc::new(PeerMedia {
            id: peer_id,
            room: room.clone(),
            nick: nick.clone(),
            pc: Arc::clone(&pc),
            signal: signal.clone(),
            outbound: Mutex::new(HashMap::new()),
            dcs: Mutex::new(HashMap::new()),
        });

        // ICE → client
        {
            let signal = signal.clone();
            let pid = peer_id;
            pc.on_ice_candidate(Box::new(move |c| {
                let signal = signal.clone();
                Box::pin(async move {
                    let Some(c) = c else { return };
                    if let Ok(init) = c.to_json() {
                        let candidate = json!({
                            "candidate": init.candidate,
                            "sdpMid": init.sdp_mid,
                            "sdpMLineIndex": init.sdp_mline_index,
                            "usernameFragment": init.username_fragment,
                        });
                        let _ = signal.send(ServerMsg::Ice {
                            from: pid,
                            candidate,
                        });
                    }
                })
            }));
        }

        {
            let pid = peer_id;
            pc.on_peer_connection_state_change(Box::new(move |s: RTCPeerConnectionState| {
                Box::pin(async move {
                    info!(%pid, ?s, "pc state");
                })
            }));
        }

        // Client-created DataChannels
        {
            let hub = Arc::clone(self);
            let sess = Arc::clone(&session);
            pc.on_data_channel(Box::new(move |dc: Arc<RTCDataChannel>| {
                let hub = Arc::clone(&hub);
                let sess = Arc::clone(&sess);
                Box::pin(async move {
                    let label = dc.label().to_string();
                    info!(peer = %sess.id, %label, "datachannel open (remote/client)");
                    register_dc(hub, sess, dc, label).await;
                })
            }));
        }

        // SFU-created outbound DataChannels (negotiated in answer)
        for label in OUTBOUND_DC_LABELS {
            match pc.create_data_channel(*label, None).await {
                Ok(dc) => {
                    info!(peer = %peer_id, label, "datachannel created (outbound SFU)");
                    register_dc(Arc::clone(self), Arc::clone(&session), dc, (*label).to_string())
                        .await;
                }
                Err(e) => warn!(peer = %peer_id, label, "create_data_channel: {e}"),
            }
        }

        // Media tracks → fan-out
        {
            let hub = Arc::clone(self);
            let pub_id = peer_id;
            let room = room.clone();
            pc.on_track(Box::new(move |track, _recv, _tr| {
                let hub = Arc::clone(&hub);
                let room = room.clone();
                Box::pin(async move {
                    if let Err(e) = hub.fanout_track(pub_id, &room, track).await {
                        warn!(%pub_id, "fanout_track: {e}");
                    }
                })
            }));
        }

        self.peers.lock().await.insert(peer_id, session);
        info!(%peer_id, %room, %nick, "media peer ready (outbound DCs: glyph|hex|chat)");
        Ok(())
    }

    pub async fn remove_peer(&self, peer_id: Uuid) {
        let sess = self.peers.lock().await.remove(&peer_id);
        if let Some(s) = sess {
            let _ = s.pc.close().await;
            info!(%peer_id, "media peer closed");
        }
    }

    pub async fn handle_offer(
        self: &Arc<Self>,
        peer_id: Uuid,
        sdp: String,
    ) -> Result<(), String> {
        let sess = {
            let g = self.peers.lock().await;
            g.get(&peer_id)
                .cloned()
                .ok_or_else(|| "no media peer — join first".to_string())?
        };

        let offer = RTCSessionDescription::offer(sdp).map_err(|e| e.to_string())?;
        sess.pc
            .set_remote_description(offer)
            .await
            .map_err(|e| e.to_string())?;

        let answer = sess
            .pc
            .create_answer(None)
            .await
            .map_err(|e| e.to_string())?;
        let mut gather = sess.pc.gathering_complete_promise().await;
        sess.pc
            .set_local_description(answer)
            .await
            .map_err(|e| e.to_string())?;
        let _ = gather.recv().await;

        let local = sess
            .pc
            .local_description()
            .await
            .ok_or_else(|| "no local description".to_string())?;

        let _ = sess.signal.send(ServerMsg::Answer {
            from: peer_id,
            sdp: local.sdp,
        });
        Ok(())
    }

    pub async fn handle_answer(&self, peer_id: Uuid, sdp: String) -> Result<(), String> {
        let sess = {
            let g = self.peers.lock().await;
            g.get(&peer_id)
                .cloned()
                .ok_or_else(|| "no media peer".to_string())?
        };
        let answer = RTCSessionDescription::answer(sdp).map_err(|e| e.to_string())?;
        sess.pc
            .set_remote_description(answer)
            .await
            .map_err(|e| e.to_string())?;
        Ok(())
    }

    pub async fn handle_ice(
        &self,
        peer_id: Uuid,
        candidate: serde_json::Value,
    ) -> Result<(), String> {
        let sess = {
            let g = self.peers.lock().await;
            g.get(&peer_id)
                .cloned()
                .ok_or_else(|| "no media peer".to_string())?
        };
        let cand = candidate
            .get("candidate")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string();
        if cand.is_empty() {
            return Ok(());
        }
        let init = RTCIceCandidateInit {
            candidate: cand,
            sdp_mid: candidate
                .get("sdpMid")
                .and_then(|v| v.as_str())
                .map(|s| s.to_owned()),
            sdp_mline_index: candidate
                .get("sdpMLineIndex")
                .and_then(|v| v.as_u64())
                .map(|n| n as u16),
            username_fragment: candidate
                .get("usernameFragment")
                .and_then(|v| v.as_str())
                .map(|s| s.to_owned()),
        };
        sess.pc
            .add_ice_candidate(init)
            .await
            .map_err(|e| e.to_string())?;
        Ok(())
    }

    async fn fanout_track(
        self: &Arc<Self>,
        publisher: Uuid,
        room: &str,
        track: Arc<TrackRemote>,
    ) -> Result<(), String> {
        let codec = track.codec();
        let cap = RTCRtpCodecCapability {
            mime_type: codec.capability.mime_type.clone(),
            clock_rate: codec.capability.clock_rate,
            channels: codec.capability.channels,
            sdp_fmtp_line: codec.capability.sdp_fmtp_line.clone(),
            rtcp_feedback: codec.capability.rtcp_feedback.clone(),
        };
        let track_id = track.id();
        let stream_id = track.stream_id();
        let key = format!("{publisher}:{track_id}");

        info!(%publisher, %track_id, mime = %cap.mime_type, "track published");

        let others: Vec<Arc<PeerMedia>> = {
            let g = self.peers.lock().await;
            g.values()
                .filter(|p| p.id != publisher && p.room == room)
                .cloned()
                .collect()
        };

        let mut locals: Vec<Arc<TrackLocalStaticRTP>> = Vec::new();
        for peer in &others {
            let local = Arc::new(TrackLocalStaticRTP::new(
                cap.clone(),
                track_id.clone(),
                stream_id.clone(),
            ));
            match peer
                .pc
                .add_track(Arc::clone(&local) as Arc<dyn TrackLocal + Send + Sync>)
                .await
            {
                Ok(_sender) => {
                    peer.outbound
                        .lock()
                        .await
                        .insert(key.clone(), Arc::clone(&local));
                    locals.push(local);
                    if let Err(e) = renegotiate_subscriber(peer).await {
                        warn!(peer = %peer.id, "renegotiate: {e}");
                    }
                }
                Err(e) => warn!(peer = %peer.id, "add_track: {e}"),
            }
        }

        loop {
            match track.read_rtp().await {
                Ok((rtp, _)) => {
                    for local in &locals {
                        let _ = local.write_rtp(&rtp).await;
                    }
                }
                Err(e) => {
                    info!(%publisher, %track_id, "track end: {e}");
                    break;
                }
            }
        }
        Ok(())
    }

    pub async fn fanout_lane_json(&self, from: Uuid, room: &str, msg: ServerMsg) {
        let g = self.peers.lock().await;
        for p in g.values() {
            if p.id == from || p.room != room {
                continue;
            }
            let _ = p.signal.send(msg.clone());
        }
    }

    /// Send raw text on outbound DataChannels with `label` to all other peers in room.
    pub async fn dc_broadcast_text(&self, from: Uuid, room: &str, label: &str, raw: &str) {
        let peers: Vec<Arc<PeerMedia>> = {
            let g = self.peers.lock().await;
            g.values()
                .filter(|p| p.id != from && p.room == room)
                .cloned()
                .collect()
        };
        for p in peers {
            let dc = {
                let dcs = p.dcs.lock().await;
                dcs.get(label).cloned()
            };
            if let Some(dc) = dc {
                if let Err(e) = dc.send_text(raw.to_string()).await {
                    warn!(peer = %p.id, %label, "dc send_text: {e}");
                }
            }
        }
    }

    pub async fn dc_broadcast_bin(&self, from: Uuid, room: &str, label: &str, data: &[u8]) {
        let peers: Vec<Arc<PeerMedia>> = {
            let g = self.peers.lock().await;
            g.values()
                .filter(|p| p.id != from && p.room == room)
                .cloned()
                .collect()
        };
        let bytes = Bytes::copy_from_slice(data);
        for p in peers {
            let dc = {
                let dcs = p.dcs.lock().await;
                dcs.get(label).cloned()
            };
            if let Some(dc) = dc {
                if let Err(e) = dc.send(&bytes).await {
                    warn!(peer = %p.id, %label, "dc send bin: {e}");
                }
            }
        }
    }
}

async fn register_dc(
    hub: Arc<MediaHub>,
    sess: Arc<PeerMedia>,
    dc: Arc<RTCDataChannel>,
    label: String,
) {
    let key = label.to_ascii_lowercase();
    {
        let mut dcs = sess.dcs.lock().await;
        dcs.insert(key.clone(), Arc::clone(&dc));
    }

    let hub2 = Arc::clone(&hub);
    let sess2 = Arc::clone(&sess);
    let key2 = key.clone();
    dc.on_open(Box::new(move || {
        let sess2 = Arc::clone(&sess2);
        let key2 = key2.clone();
        Box::pin(async move {
            info!(peer = %sess2.id, label = %key2, "datachannel open");
        })
    }));

    dc.on_message(Box::new(move |msg: DataChannelMessage| {
        let hub = Arc::clone(&hub2);
        let sess = Arc::clone(&sess);
        let label_l = key.clone();
        Box::pin(async move {
            if msg.is_string {
                let raw = String::from_utf8_lossy(&msg.data).into_owned();
                handle_dc_message(hub, sess, &label_l, &raw).await;
            } else {
                // Binary glyph grids: fan-out raw + WS notify
                let n = if msg.data.len() == 13 * 13 {
                    13
                } else if msg.data.len() == 25 * 25 {
                    25
                } else {
                    0
                };
                hub.fanout_lane_json(
                    sess.id,
                    &sess.room,
                    ServerMsg::Glyph {
                        from: sess.id,
                        n: if n == 0 { 25 } else { n },
                        data: msg.data.to_vec(),
                    },
                )
                .await;
                hub.dc_broadcast_bin(sess.id, &sess.room, &label_l, &msg.data)
                    .await;
            }
        })
    }));
}

async fn renegotiate_subscriber(peer: &PeerMedia) -> Result<(), String> {
    let offer = peer
        .pc
        .create_offer(None)
        .await
        .map_err(|e| e.to_string())?;
    let mut gather = peer.pc.gathering_complete_promise().await;
    peer.pc
        .set_local_description(offer)
        .await
        .map_err(|e| e.to_string())?;
    let _ = gather.recv().await;
    let local = peer
        .pc
        .local_description()
        .await
        .ok_or_else(|| "no local desc".to_string())?;
    let _ = peer.signal.send(ServerMsg::Offer {
        from: peer.id,
        sdp: local.sdp,
    });
    Ok(())
}

async fn handle_dc_message(hub: Arc<MediaHub>, sess: Arc<PeerMedia>, label: &str, raw: &str) {
    let v: serde_json::Value = match serde_json::from_str(raw) {
        Ok(v) => v,
        Err(_) => {
            let msg = match label {
                "chat" => ServerMsg::Chat {
                    from: sess.id,
                    nick: sess.nick.clone(),
                    text: raw.to_string(),
                    t: now_ms(),
                    role: None,
                    meta: Some(json!({"via": "datachannel"})),
                },
                "hex" => ServerMsg::Hex {
                    from: sess.id,
                    payload: raw.to_string(),
                },
                _ => ServerMsg::Glyph {
                    from: sess.id,
                    n: 25,
                    data: raw.as_bytes().to_vec(),
                },
            };
            hub.fanout_lane_json(sess.id, &sess.room, msg).await;
            hub.dc_broadcast_text(sess.id, &sess.room, label, raw).await;
            return;
        }
    };

    let typ = v.get("type").and_then(|t| t.as_str()).unwrap_or(label);
    let server = match typ {
        "chat" => {
            let text = v
                .get("text")
                .and_then(|t| t.as_str())
                .unwrap_or("")
                .to_string();
            if text.is_empty() {
                return;
            }
            ServerMsg::Chat {
                from: sess.id,
                nick: v
                    .get("from")
                    .and_then(|t| t.as_str())
                    .unwrap_or(&sess.nick)
                    .to_string(),
                text,
                t: now_ms(),
                role: v
                    .get("role")
                    .and_then(|t| t.as_str())
                    .map(|s| s.to_string()),
                meta: v.get("meta").cloned(),
            }
        }
        "hex" => ServerMsg::Hex {
            from: sess.id,
            payload: v
                .get("payload")
                .and_then(|t| t.as_str())
                .unwrap_or(raw)
                .to_string(),
        },
        _ => {
            let n = v.get("n").and_then(|x| x.as_u64()).unwrap_or(25) as u32;
            let data = if let Some(arr) = v.get("data").and_then(|d| d.as_array()) {
                arr.iter()
                    .filter_map(|x| x.as_u64().map(|n| n as u8))
                    .collect()
            } else {
                raw.as_bytes().to_vec()
            };
            ServerMsg::Glyph {
                from: sess.id,
                n,
                data,
            }
        }
    };

    hub.fanout_lane_json(sess.id, &sess.room, server).await;
    hub.dc_broadcast_text(sess.id, &sess.room, label, raw).await;
}

fn now_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}
