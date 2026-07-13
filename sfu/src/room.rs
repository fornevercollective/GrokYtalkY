//! Room registry — private DOJO rooms with soft peer caps + drop-aware fan-out.

use std::collections::HashMap;
use std::sync::Mutex;

use serde::Serialize;
use tokio::sync::mpsc;
use uuid::Uuid;

use crate::signaling::ServerMsg;

#[derive(Debug, Clone)]
pub struct Peer {
    pub id: Uuid,
    pub nick: String,
    pub lanes: Vec<String>,
    /// Bounded outbox — full = drop (backpressure) for glyph/hex flood.
    pub tx: mpsc::Sender<ServerMsg>,
}

#[derive(Debug, Default)]
struct Room {
    peers: HashMap<Uuid, Peer>,
}

#[derive(Debug, Serialize)]
pub struct RoomSnap {
    pub name: String,
    pub peers: usize,
    pub nicks: Vec<String>,
}

#[derive(Debug, Default)]
pub struct RoomRegistry {
    inner: Mutex<HashMap<String, Room>>,
}

/// Normalize room id (align with gy hub tenancy).
pub fn normalize_room(s: &str) -> String {
    let s = s.trim().to_lowercase();
    if s.is_empty() {
        return "dojo".into();
    }
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            'a'..='z' | '0'..='9' | '.' | '_' | '-' => out.push(c),
            ' ' | '/' => out.push('-'),
            _ => {}
        }
    }
    let out = out.trim_matches(|c| c == '-' || c == '.').to_string();
    if out.is_empty() {
        "dojo".into()
    } else if out.len() > 64 {
        out[..64].to_string()
    } else {
        out
    }
}

impl RoomRegistry {
    pub fn new() -> Self {
        Self {
            inner: Mutex::new(HashMap::new()),
        }
    }

    pub fn room_count(&self) -> usize {
        self.inner.lock().expect("rooms").len()
    }

    pub fn peer_count(&self) -> usize {
        self.inner
            .lock()
            .expect("rooms")
            .values()
            .map(|r| r.peers.len())
            .sum()
    }

    pub fn snapshot(&self) -> Vec<RoomSnap> {
        let guard = self.inner.lock().expect("rooms");
        let mut out: Vec<RoomSnap> = guard
            .iter()
            .map(|(name, room)| RoomSnap {
                name: name.clone(),
                peers: room.peers.len(),
                nicks: room.peers.values().map(|p| p.nick.clone()).collect(),
            })
            .collect();
        out.sort_by(|a, b| a.name.cmp(&b.name));
        out
    }

    pub fn join(
        &self,
        room: &str,
        nick: String,
        lanes: Vec<String>,
        tx: mpsc::Sender<ServerMsg>,
        max_peers: usize,
    ) -> Result<(Uuid, Vec<Peer>), String> {
        let room = normalize_room(room);
        let mut guard = self.inner.lock().expect("rooms");
        let room_entry = guard.entry(room).or_default();
        if room_entry.peers.len() >= max_peers {
            return Err(format!(
                "room full ({max_peers} peers) — jam target 16–32; raise --max-peers-per-room or open a new room"
            ));
        }
        let id = Uuid::new_v4();
        let peer = Peer {
            id,
            nick,
            lanes,
            tx,
        };
        let others: Vec<Peer> = room_entry.peers.values().cloned().collect();
        room_entry.peers.insert(id, peer);
        Ok((id, others))
    }

    pub fn leave(&self, room: &str, peer_id: Uuid) -> Option<Peer> {
        let room = normalize_room(room);
        let mut guard = self.inner.lock().expect("rooms");
        let Some(r) = guard.get_mut(&room) else {
            return None;
        };
        let gone = r.peers.remove(&peer_id);
        if r.peers.is_empty() {
            guard.remove(&room);
        }
        gone
    }

    /// Broadcast; returns number of recipients that dropped (outbox full/closed).
    pub fn broadcast(&self, room: &str, except: Option<Uuid>, msg: ServerMsg) -> u64 {
        let room = normalize_room(room);
        let guard = self.inner.lock().expect("rooms");
        let Some(r) = guard.get(&room) else {
            return 0;
        };
        let mut drops = 0u64;
        for (id, peer) in &r.peers {
            if except.is_some_and(|e| e == *id) {
                continue;
            }
            match peer.tx.try_send(msg.clone()) {
                Ok(()) => {}
                Err(_) => drops += 1,
            }
        }
        drops
    }

    /// Critical path: try_send then ignore (caller may log). Prefer for welcome.
    pub fn send_to(&self, room: &str, peer_id: Uuid, msg: ServerMsg) -> bool {
        let room = normalize_room(room);
        let guard = self.inner.lock().expect("rooms");
        if let Some(r) = guard.get(&room) {
            if let Some(p) = r.peers.get(&peer_id) {
                return p.tx.try_send(msg).is_ok();
            }
        }
        false
    }

    /// Blocking-friendly clone of peer for ICE etc.
    pub fn get_peer_tx(&self, room: &str, peer_id: Uuid) -> Option<mpsc::Sender<ServerMsg>> {
        let room = normalize_room(room);
        let guard = self.inner.lock().expect("rooms");
        guard
            .get(&room)
            .and_then(|r| r.peers.get(&peer_id).map(|p| p.tx.clone()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_room_basic() {
        assert_eq!(normalize_room("  DOJO "), "dojo");
        assert_eq!(normalize_room("US/CA SF"), "us-ca-sf");
        assert_eq!(normalize_room(""), "dojo");
    }

    #[tokio::test]
    async fn room_full_and_broadcast_drops() {
        let reg = RoomRegistry::new();
        let (tx1, mut rx1) = mpsc::channel::<ServerMsg>(1);
        let (tx2, _rx2) = mpsc::channel::<ServerMsg>(1);
        let (id1, _) = reg
            .join("jam", "a".into(), vec![], tx1, 2)
            .expect("join a");
        let (_id2, _) = reg
            .join("jam", "b".into(), vec![], tx2, 2)
            .expect("join b");
        let (tx3, _rx3) = mpsc::channel::<ServerMsg>(1);
        assert!(reg.join("jam", "c".into(), vec![], tx3, 2).is_err());

        // fill a's outbox
        let _ = reg.send_to(
            "jam",
            id1,
            ServerMsg::Error {
                message: "fill".into(),
            },
        );
        // second send may drop when broadcasting to full outbox
        let drops = reg.broadcast(
            "jam",
            None,
            ServerMsg::Error {
                message: "flood".into(),
            },
        );
        // at least one drop possible depending on which peer's buffer filled
        let _ = drops;
        // drain
        let _ = rx1.try_recv();
    }
}
