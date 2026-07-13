//! WebSocket signaling — SDP/ICE relay + glyph/hex lane fan-out.

use std::sync::Arc;

use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::extract::{Query, State};
use axum::response::IntoResponse;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use tokio::sync::mpsc;
use uuid::Uuid;

use crate::lanes;
use crate::AppState;

#[derive(Debug, Deserialize)]
pub struct WsQuery {
    #[serde(default = "default_room")]
    pub room: String,
    #[serde(default = "default_nick")]
    pub nick: String,
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
    /// Glyph matrix lane (brightness or future RGB triples as JSON array).
    Glyph {
        n: u32,
        data: Vec<u8>,
    },
    /// Opaque hex / packet lane payload (base64 or raw string).
    Hex {
        #[serde(default)]
        payload: String,
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
        data: Vec<u8>,
    },
    Hex {
        from: Uuid,
        payload: String,
    },
    Error {
        message: String,
    },
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
    let (tx, mut rx) = mpsc::unbounded_channel::<ServerMsg>();

    // outbound task
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

    let mut room = q.room.clone();
    let mut nick = q.nick.clone();
    let mut peer_lanes = lanes::default_dojo_lanes();
    let mut peer_id: Option<Uuid> = None;
    let rooms = Arc::clone(&state.rooms);

    // Auto-join from query string so `ws://host/ws?room=dojo&nick=qbit` works immediately.
    match rooms.join(
        &room,
        nick.clone(),
        peer_lanes.clone(),
        tx.clone(),
        state.max_peers_per_room,
    ) {
        Ok((id, others)) => {
            peer_id = Some(id);
            let _ = tx.send(ServerMsg::Welcome {
                peer_id: id,
                room: room.clone(),
                media: state.media_enabled,
                lanes: peer_lanes.clone(),
            });
            for o in &others {
                let _ = tx.send(ServerMsg::PeerJoined {
                    peer_id: o.id,
                    nick: o.nick.clone(),
                    lanes: o.lanes.clone(),
                });
            }
            rooms.broadcast(
                &room,
                Some(id),
                ServerMsg::PeerJoined {
                    peer_id: id,
                    nick: nick.clone(),
                    lanes: peer_lanes.clone(),
                },
            );
            tracing::info!(%id, %room, %nick, "peer joined (query)");
        }
        Err(message) => {
            let _ = tx.send(ServerMsg::Error { message });
        }
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

        match cmsg {
            ClientMsg::Join {
                room: r,
                nick: n,
                lanes: l,
            } => {
                if let Some(r) = r {
                    if !r.is_empty() {
                        room = r;
                    }
                }
                if let Some(n) = n {
                    if !n.is_empty() {
                        nick = n;
                    }
                }
                if let Some(l) = l {
                    peer_lanes = lanes::normalize(&l);
                }

                // re-join: leave previous
                if let Some(id) = peer_id.take() {
                    rooms.leave(&room, id);
                    rooms.broadcast(
                        &room,
                        None,
                        ServerMsg::PeerLeft { peer_id: id },
                    );
                }

                match rooms.join(
                    &room,
                    nick.clone(),
                    peer_lanes.clone(),
                    tx.clone(),
                    state.max_peers_per_room,
                ) {
                    Ok((id, others)) => {
                        peer_id = Some(id);
                        let _ = tx.send(ServerMsg::Welcome {
                            peer_id: id,
                            room: room.clone(),
                            media: state.media_enabled,
                            lanes: peer_lanes.clone(),
                        });
                        for o in &others {
                            let _ = tx.send(ServerMsg::PeerJoined {
                                peer_id: o.id,
                                nick: o.nick.clone(),
                                lanes: o.lanes.clone(),
                            });
                        }
                        rooms.broadcast(
                            &room,
                            Some(id),
                            ServerMsg::PeerJoined {
                                peer_id: id,
                                nick: nick.clone(),
                                lanes: peer_lanes.clone(),
                            },
                        );
                        tracing::info!(%id, %room, %nick, "peer joined");
                    }
                    Err(message) => {
                        let _ = tx.send(ServerMsg::Error { message });
                    }
                }
            }
            ClientMsg::Offer { sdp, to } => {
                let Some(from) = peer_id else {
                    let _ = tx.send(ServerMsg::Error {
                        message: "join first".into(),
                    });
                    continue;
                };
                let msg = ServerMsg::Offer { from, sdp };
                if let Some(to) = to {
                    rooms.send_to(&room, to, msg);
                } else {
                    rooms.broadcast(&room, Some(from), msg);
                }
            }
            ClientMsg::Answer { sdp, to } => {
                let Some(from) = peer_id else { continue };
                let msg = ServerMsg::Answer { from, sdp };
                if let Some(to) = to {
                    rooms.send_to(&room, to, msg);
                } else {
                    rooms.broadcast(&room, Some(from), msg);
                }
            }
            ClientMsg::Ice { candidate, to } => {
                let Some(from) = peer_id else { continue };
                let msg = ServerMsg::Ice { from, candidate };
                if let Some(to) = to {
                    rooms.send_to(&room, to, msg);
                } else {
                    rooms.broadcast(&room, Some(from), msg);
                }
            }
            ClientMsg::Glyph { n, data } => {
                let Some(from) = peer_id else { continue };
                // Soft sanity: device N or display ladder
                if !(n == 13 || n == 25 || n == 37 || n == 49 || n <= 96) {
                    let _ = tx.send(ServerMsg::Error {
                        message: format!("glyph n={n} unsupported"),
                    });
                    continue;
                }
                rooms.broadcast(
                    &room,
                    Some(from),
                    ServerMsg::Glyph { from, n, data },
                );
            }
            ClientMsg::Hex { payload } => {
                let Some(from) = peer_id else { continue };
                rooms.broadcast(
                    &room,
                    Some(from),
                    ServerMsg::Hex { from, payload },
                );
            }
            ClientMsg::Leave => break,
            ClientMsg::Unknown => {
                let _ = tx.send(ServerMsg::Error {
                    message: "unknown message type".into(),
                });
            }
        }
    }

    if let Some(id) = peer_id.take() {
        rooms.leave(&room, id);
        rooms.broadcast(&room, None, ServerMsg::PeerLeft { peer_id: id });
        tracing::info!(%id, %room, "peer left");
    }
    out.abort();
}
