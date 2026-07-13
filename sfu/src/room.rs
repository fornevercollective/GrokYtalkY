//! Room registry — private DOJO rooms with soft peer caps.

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
    pub tx: mpsc::UnboundedSender<ServerMsg>,
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
        tx: mpsc::UnboundedSender<ServerMsg>,
        max_peers: usize,
    ) -> Result<(Uuid, Vec<Peer>), String> {
        let mut guard = self.inner.lock().expect("rooms");
        let room_entry = guard.entry(room.to_string()).or_default();
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
        // others already in room
        let others: Vec<Peer> = room_entry.peers.values().cloned().collect();
        room_entry.peers.insert(id, peer);
        Ok((id, others))
    }

    pub fn leave(&self, room: &str, peer_id: Uuid) -> Option<Peer> {
        let mut guard = self.inner.lock().expect("rooms");
        let Some(r) = guard.get_mut(room) else {
            return None;
        };
        let gone = r.peers.remove(&peer_id);
        if r.peers.is_empty() {
            guard.remove(room);
        }
        gone
    }

    /// Broadcast to everyone in room except `except`.
    pub fn broadcast(&self, room: &str, except: Option<Uuid>, msg: ServerMsg) {
        let guard = self.inner.lock().expect("rooms");
        let Some(r) = guard.get(room) else {
            return;
        };
        for (id, peer) in &r.peers {
            if except.is_some_and(|e| e == *id) {
                continue;
            }
            let _ = peer.tx.send(msg.clone());
        }
    }

    pub fn send_to(&self, room: &str, peer_id: Uuid, msg: ServerMsg) {
        let guard = self.inner.lock().expect("rooms");
        if let Some(r) = guard.get(room) {
            if let Some(p) = r.peers.get(&peer_id) {
                let _ = p.tx.send(msg);
            }
        }
    }
}
