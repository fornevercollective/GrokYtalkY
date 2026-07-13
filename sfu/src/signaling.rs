//! WebSocket signaling — SDP/ICE relay + glyph/hex/chat + optional token auth.
//! Jam-scale: bounded outbox, glyph rate/size limits, room normalize, metrics.

use std::sync::Arc;
use std::time::{Duration, Instant};

use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::extract::{Query, State};
use axum::response::IntoResponse;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use tokio::sync::mpsc;
use uuid::Uuid;

use crate::lanes;
use crate::metrics::Metrics;
use crate::room::{normalize_room, RoomRegistry};
use crate::AppState;

#[derive(Debug, Deserialize)]
pub struct WsQuery {
    #[serde(default = "default_room")]
    pub room: String,
    #[serde(default = "default_nick")]
    pub nick: String,
    /// Shared room token when SFU started with --token / GY_SFU_TOKEN
    #[serde(default)]
    pub token: String,
}

fn default_room() -> String {
    "dojo".into()
}
fn default_nick() -> String {
    "anon".into()
}

/// Client → SFU
#[derive(Debug, Clone, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ClientMsg {
    Join {
        #[serde(default)]
        room: Option<String>,
        #[serde(default)]
        nick: Option<String>,
        #[serde(default)]
        lanes: Option<Vec<String>>,
        #[serde(default)]
        token: Option<String>,
    },
    Offer {
        sdp: String,
        #[serde(default)]
        to: Option<Uuid>,
    },
    Answer {
        sdp: String,
        #[serde(default)]
        to: Option<Uuid>,
    },
    Ice {
        candidate: serde_json::Value,
        #[serde(default)]
        to: Option<Uuid>,
    },
    Glyph {
        n: u32,
        /// luminance grid — JSON number array (not base64)
        #[serde(with = "u8_seq")]
        data: Vec<u8>,
    },
    Hex {
        #[serde(default)]
        payload: String,
    },
    Chat {
        text: String,
        #[serde(default)]
        from: Option<String>,
        #[serde(default)]
        role: Option<String>,
        #[serde(default)]
        meta: Option<serde_json::Value>,
    },
    Leave,
    #[serde(other)]
    Unknown,
}

/// SFU → client
#[derive(Debug, Clone, Serialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ServerMsg {
    Welcome {
        peer_id: Uuid,
        room: String,
        media: bool,
        lanes: Vec<String>,
        /// true when SFU was started with --token / GY_SFU_TOKEN
        #[serde(default)]
        auth: bool,
    },
    PeerJoined {
        peer_id: Uuid,
        nick: String,
        lanes: Vec<String>,
    },
    PeerLeft {
        peer_id: Uuid,
    },
    Offer {
        from: Uuid,
        sdp: String,
    },
    Answer {
        from: Uuid,
        sdp: String,
    },
    Ice {
        from: Uuid,
        candidate: serde_json::Value,
    },
    Glyph {
        from: Uuid,
        n: u32,
        /// luminance grid as JSON number array (matches bridge + dojo.html)
        #[serde(with = "u8_seq")]
        data: Vec<u8>,
    },
    Hex {
        from: Uuid,
        payload: String,
    },
    Chat {
        from: Uuid,
        nick: String,
        text: String,
        t: i64,
        role: Option<String>,
        meta: Option<serde_json::Value>,
    },
    Error {
        message: String,
    },
}

/// Serialize/deserialize Vec<u8> as JSON number arrays (not base64).
mod u8_seq {
    use serde::de::{self, SeqAccess, Visitor};
    use serde::{Deserializer, Serializer};
    use std::fmt;

    pub fn serialize<S>(data: &Vec<u8>, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        use serde::ser::SerializeSeq;
        let mut seq = serializer.serialize_seq(Some(data.len()))?;
        for b in data {
            seq.serialize_element(b)?;
        }
        seq.end()
    }

    pub fn deserialize<'de, D>(deserializer: D) -> Result<Vec<u8>, D::Error>
    where
        D: Deserializer<'de>,
    {
        struct U8SeqVisitor;
        impl<'de> Visitor<'de> for U8SeqVisitor {
            type Value = Vec<u8>;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("byte array as JSON numbers or base64 string")
            }
            fn visit_seq<A>(self, mut seq: A) -> Result<Self::Value, A::Error>
            where
                A: SeqAccess<'de>,
            {
                let mut out = Vec::with_capacity(seq.size_hint().unwrap_or(0));
                while let Some(v) = seq.next_element::<u64>()? {
                    out.push((v & 0xff) as u8);
                }
                Ok(out)
            }
            fn visit_bytes<E>(self, v: &[u8]) -> Result<Self::Value, E>
            where
                E: de::Error,
            {
                Ok(v.to_vec())
            }
        }
        deserializer.deserialize_any(U8SeqVisitor)
    }
}

pub async fn ws_handler(
    ws: WebSocketUpgrade,
    Query(q): Query<WsQuery>,
    State(state): State<AppState>,
) -> impl IntoResponse {
    ws.on_upgrade(move |socket| peer_session(socket, state, q))
}

async fn peer_session(socket: WebSocket, state: AppState, q: WsQuery) {
    let (mut sink, mut stream) = socket.split();
    let (tx, mut rx) = mpsc::channel::<ServerMsg>(state.outbox_capacity);

    let metrics = Arc::clone(&state.metrics);
    let out = tokio::spawn(async move {
        while let Some(msg) = rx.recv().await {
            match serde_json::to_string(&msg) {
                Ok(s) => {
                    if sink.send(Message::Text(s.into())).await.is_err() {
                        break;
                    }
                }
                Err(e) => {
                    tracing::warn!("serialize: {e}");
                    break;
                }
            }
        }
    });

    let mut room = normalize_room(&q.room);
    let mut nick = q.nick.clone();
    let mut peer_lanes = lanes::default_dojo_lanes();
    let mut peer_id: Option<Uuid> = None;
    let rooms = Arc::clone(&state.rooms);
    let mut authed = check_token(&state.token, &q.token);
    // per-peer glyph rate limit
    let mut last_glyph: Option<Instant> = None;

    if !state.token.is_empty() && !authed {
        Metrics::inc(&state.metrics.auth_fail);
        let _ = tx
            .try_send(ServerMsg::Error {
                message: "auth required: ?token= or join.token (GY_SFU_TOKEN)".into(),
            })
            .ok();
    } else if let Err(message) = try_join(
        &rooms,
        &state,
        &mut room,
        &mut nick,
        &peer_lanes,
        &tx,
        &mut peer_id,
    )
    .await
    {
        Metrics::inc(&state.metrics.joins_fail);
        let _ = tx.try_send(ServerMsg::Error { message }).ok();
    }

    while let Some(Ok(msg)) = stream.next().await {
        let text = match msg {
            Message::Text(t) => t.to_string(),
            Message::Binary(b) => String::from_utf8_lossy(&b).into_owned(),
            Message::Close(_) => break,
            Message::Ping(_) | Message::Pong(_) => continue,
        };

        let cmsg: ClientMsg = match serde_json::from_str(&text) {
            Ok(m) => m,
            Err(e) => {
                let _ = tx.send(ServerMsg::Error {
                    message: format!("bad json: {e}"),
                });
                continue;
            }
        };

        if let ClientMsg::Join {
            room: r,
            nick: n,
            lanes: l,
            token: join_tok,
        } = &cmsg
        {
            if let Some(t) = join_tok {
                if check_token(&state.token, t) {
                    authed = true;
                }
            }
            // query token already checked; allow re-auth via Join
            if !state.token.is_empty() && !authed {
                Metrics::inc(&state.metrics.auth_fail);
                let _ = tx
                    .try_send(ServerMsg::Error {
                        message: "auth failed".into(),
                    })
                    .ok();
                continue;
            }
            let old_room = room.clone();
            if let Some(r) = r {
                if !r.is_empty() {
                    room = normalize_room(r);
                }
            }
            if let Some(n) = n {
                if !n.is_empty() {
                    nick = n.clone();
                }
            }
            if let Some(l) = l {
                peer_lanes = lanes::normalize(l);
            }
            if let Some(id) = peer_id.take() {
                media_remove(&state, id).await;
                rooms.leave(&old_room, id);
                let d = rooms.broadcast(&old_room, None, ServerMsg::PeerLeft { peer_id: id });
                Metrics::add(&state.metrics.outbox_drops, d);
                Metrics::inc(&state.metrics.leaves);
            }
            if let Err(message) = try_join(
                &rooms,
                &state,
                &mut room,
                &mut nick,
                &peer_lanes,
                &tx,
                &mut peer_id,
            )
            .await
            {
                Metrics::inc(&state.metrics.joins_fail);
                let _ = tx.try_send(ServerMsg::Error { message }).ok();
            }
            continue;
        }

        if !state.token.is_empty() && !authed {
            Metrics::inc(&state.metrics.auth_fail);
            let _ = tx
                .try_send(ServerMsg::Error {
                    message: "auth required before media/signaling".into(),
                })
                .ok();
            continue;
        }

        if matches!(cmsg, ClientMsg::Leave) {
            break;
        }

        handle_authed(
            &state,
            &rooms,
            &room,
            &nick,
            &mut peer_id,
            &tx,
            &mut last_glyph,
            cmsg,
        )
        .await;
    }

    if let Some(id) = peer_id.take() {
        media_remove(&state, id).await;
        rooms.leave(&room, id);
        let d = rooms.broadcast(&room, None, ServerMsg::PeerLeft { peer_id: id });
        Metrics::add(&metrics.outbox_drops, d);
        Metrics::inc(&metrics.leaves);
        tracing::info!(%id, %room, "peer left");
    }
    out.abort();
}

fn check_token(required: &str, provided: &str) -> bool {
    if required.is_empty() {
        return true;
    }
    let a = required.as_bytes();
    let b = provided.as_bytes();
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

async fn try_join(
    rooms: &Arc<RoomRegistry>,
    state: &AppState,
    room: &mut String,
    nick: &mut String,
    peer_lanes: &[String],
    tx: &mpsc::Sender<ServerMsg>,
    peer_id: &mut Option<Uuid>,
) -> Result<(), String> {
    *room = normalize_room(room);
    if state.max_peers_node > 0 && rooms.peer_count() >= state.max_peers_node {
        return Err(format!(
            "node full ({} peers) — start another gy-sfu or raise --max-peers-node",
            state.max_peers_node
        ));
    }
    match rooms.join(
        room,
        nick.clone(),
        peer_lanes.to_vec(),
        tx.clone(),
        state.max_peers_per_room,
    ) {
        Ok((id, others)) => {
            *peer_id = Some(id);
            media_ensure(state, id, room, nick, tx.clone()).await;
            let _ = tx
                .try_send(ServerMsg::Welcome {
                    peer_id: id,
                    room: room.clone(),
                    media: state.media_enabled,
                    lanes: peer_lanes.to_vec(),
                    auth: !state.token.is_empty(),
                })
                .ok();
            for o in &others {
                let _ = tx
                    .try_send(ServerMsg::PeerJoined {
                        peer_id: o.id,
                        nick: o.nick.clone(),
                        lanes: o.lanes.clone(),
                    })
                    .ok();
            }
            let d = rooms.broadcast(
                room,
                Some(id),
                ServerMsg::PeerJoined {
                    peer_id: id,
                    nick: nick.clone(),
                    lanes: peer_lanes.to_vec(),
                },
            );
            Metrics::add(&state.metrics.outbox_drops, d);
            Metrics::inc(&state.metrics.joins_ok);
            tracing::info!(%id, %room, %nick, "peer joined");
            Ok(())
        }
        Err(message) => Err(message),
    }
}

async fn handle_authed(
    state: &AppState,
    rooms: &Arc<RoomRegistry>,
    room: &str,
    nick: &str,
    peer_id: &mut Option<Uuid>,
    tx: &mpsc::Sender<ServerMsg>,
    last_glyph: &mut Option<Instant>,
    cmsg: ClientMsg,
) {
    match cmsg {
        ClientMsg::Join { .. } | ClientMsg::Leave => {}
        ClientMsg::Offer { sdp, to } => {
            let Some(from) = *peer_id else {
                let _ = tx
                    .try_send(ServerMsg::Error {
                        message: "join first".into(),
                    })
                    .ok();
                return;
            };
            Metrics::inc(&state.metrics.offers);
            if to.is_none() && state.media_enabled {
                if let Err(e) = media_handle_offer(state, from, sdp).await {
                    let _ = tx.try_send(ServerMsg::Error { message: e }).ok();
                }
                return;
            }
            let msg = ServerMsg::Offer { from, sdp };
            let d = if let Some(to) = to {
                if rooms.send_to(room, to, msg) {
                    0
                } else {
                    1
                }
            } else {
                rooms.broadcast(room, Some(from), msg)
            };
            Metrics::add(&state.metrics.outbox_drops, d);
        }
        ClientMsg::Answer { sdp, to } => {
            let Some(from) = *peer_id else { return };
            Metrics::inc(&state.metrics.answers);
            if to.is_none() && state.media_enabled {
                if let Err(e) = media_handle_answer(state, from, sdp).await {
                    let _ = tx.try_send(ServerMsg::Error { message: e }).ok();
                }
                return;
            }
            let msg = ServerMsg::Answer { from, sdp };
            let d = if let Some(to) = to {
                if rooms.send_to(room, to, msg) {
                    0
                } else {
                    1
                }
            } else {
                rooms.broadcast(room, Some(from), msg)
            };
            Metrics::add(&state.metrics.outbox_drops, d);
        }
        ClientMsg::Ice { candidate, to } => {
            let Some(from) = *peer_id else { return };
            Metrics::inc(&state.metrics.ice);
            if to.is_none() && state.media_enabled {
                if let Err(e) = media_handle_ice(state, from, candidate).await {
                    let _ = tx.try_send(ServerMsg::Error { message: e }).ok();
                }
                return;
            }
            let msg = ServerMsg::Ice { from, candidate };
            let d = if let Some(to) = to {
                if rooms.send_to(room, to, msg) {
                    0
                } else {
                    1
                }
            } else {
                rooms.broadcast(room, Some(from), msg)
            };
            Metrics::add(&state.metrics.outbox_drops, d);
        }
        ClientMsg::Glyph { n, data } => {
            let Some(from) = *peer_id else { return };
            if !(n == 13 || n == 25 || n == 37 || n == 49 || n <= 96) {
                let _ = tx
                    .try_send(ServerMsg::Error {
                        message: format!("glyph n={n} unsupported"),
                    })
                    .ok();
                return;
            }
            if data.len() > state.max_glyph_bytes {
                Metrics::inc(&state.metrics.glyph_size_drops);
                return;
            }
            if state.glyph_min_interval_ms > 0 {
                let min = Duration::from_millis(state.glyph_min_interval_ms);
                if let Some(prev) = *last_glyph {
                    if prev.elapsed() < min {
                        Metrics::inc(&state.metrics.glyph_rate_drops);
                        return;
                    }
                }
                *last_glyph = Some(Instant::now());
            }
            Metrics::inc(&state.metrics.glyph_msgs);
            let d = rooms.broadcast(room, Some(from), ServerMsg::Glyph { from, n, data });
            Metrics::add(&state.metrics.outbox_drops, d);
        }
        ClientMsg::Hex { payload } => {
            let Some(from) = *peer_id else { return };
            if payload.len() > state.max_hex_bytes {
                Metrics::inc(&state.metrics.glyph_size_drops);
                return;
            }
            Metrics::inc(&state.metrics.hex_msgs);
            let d = rooms.broadcast(room, Some(from), ServerMsg::Hex { from, payload });
            Metrics::add(&state.metrics.outbox_drops, d);
        }
        ClientMsg::Chat {
            text,
            from: from_nick,
            role,
            meta,
        } => {
            let Some(from) = *peer_id else { return };
            let text = text.trim();
            if text.is_empty() || text.len() > 2000 {
                return;
            }
            let nick = from_nick
                .filter(|s| !s.is_empty())
                .unwrap_or_else(|| nick.to_string());
            let t = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_millis() as i64)
                .unwrap_or(0);
            Metrics::inc(&state.metrics.chat_msgs);
            let d = rooms.broadcast(
                room,
                Some(from),
                ServerMsg::Chat {
                    from,
                    nick,
                    text: text.to_string(),
                    t,
                    role,
                    meta,
                },
            );
            Metrics::add(&state.metrics.outbox_drops, d);
        }
        ClientMsg::Unknown => {
            let _ = tx
                .try_send(ServerMsg::Error {
                    message: "unknown message type".into(),
                })
                .ok();
        }
    }
}

async fn media_ensure(
    state: &AppState,
    id: Uuid,
    room: &str,
    nick: &str,
    tx: mpsc::Sender<ServerMsg>,
) {
    #[cfg(feature = "media")]
    if let Some(hub) = &state.media {
        if let Err(e) = hub
            .ensure_peer(id, room.to_string(), nick.to_string(), tx)
            .await
        {
            tracing::warn!(%id, "media ensure: {e}");
        }
    }
    #[cfg(not(feature = "media"))]
    let _ = (state, id, room, nick, tx);
}

async fn media_remove(state: &AppState, id: Uuid) {
    #[cfg(feature = "media")]
    if let Some(hub) = &state.media {
        hub.remove_peer(id).await;
    }
    #[cfg(not(feature = "media"))]
    let _ = (state, id);
}

async fn media_handle_offer(state: &AppState, id: Uuid, sdp: String) -> Result<(), String> {
    #[cfg(feature = "media")]
    {
        let hub = state
            .media
            .as_ref()
            .ok_or_else(|| "media hub down".to_string())?;
        hub.handle_offer(id, sdp).await
    }
    #[cfg(not(feature = "media"))]
    {
        let _ = (state, id, sdp);
        Err("rebuild with --features media".into())
    }
}

async fn media_handle_answer(state: &AppState, id: Uuid, sdp: String) -> Result<(), String> {
    #[cfg(feature = "media")]
    {
        let hub = state
            .media
            .as_ref()
            .ok_or_else(|| "media hub down".to_string())?;
        hub.handle_answer(id, sdp).await
    }
    #[cfg(not(feature = "media"))]
    {
        let _ = (state, id, sdp);
        Err("rebuild with --features media".into())
    }
}

async fn media_handle_ice(
    state: &AppState,
    id: Uuid,
    candidate: serde_json::Value,
) -> Result<(), String> {
    #[cfg(feature = "media")]
    {
        let hub = state
            .media
            .as_ref()
            .ok_or_else(|| "media hub down".to_string())?;
        hub.handle_ice(id, candidate).await
    }
    #[cfg(not(feature = "media"))]
    {
        let _ = (state, id, candidate);
        Err("rebuild with --features media".into())
    }
}
