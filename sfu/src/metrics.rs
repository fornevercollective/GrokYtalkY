//! Process metrics for jam-scale ops (JSON /health + /metrics).

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use serde::Serialize;

#[derive(Debug, Default)]
pub struct Metrics {
    pub joins_ok: AtomicU64,
    pub joins_fail: AtomicU64,
    pub leaves: AtomicU64,
    pub glyph_msgs: AtomicU64,
    pub hex_msgs: AtomicU64,
    pub chat_msgs: AtomicU64,
    pub outbox_drops: AtomicU64,
    pub glyph_rate_drops: AtomicU64,
    pub glyph_size_drops: AtomicU64,
    pub offers: AtomicU64,
    pub answers: AtomicU64,
    pub ice: AtomicU64,
    pub auth_fail: AtomicU64,
}

impl Metrics {
    pub fn new() -> Arc<Self> {
        Arc::new(Self::default())
    }

    fn n(a: &AtomicU64) -> u64 {
        a.load(Ordering::Relaxed)
    }

    pub fn snap(&self) -> MetricsSnap {
        MetricsSnap {
            joins_ok: Self::n(&self.joins_ok),
            joins_fail: Self::n(&self.joins_fail),
            leaves: Self::n(&self.leaves),
            glyph_msgs: Self::n(&self.glyph_msgs),
            hex_msgs: Self::n(&self.hex_msgs),
            chat_msgs: Self::n(&self.chat_msgs),
            outbox_drops: Self::n(&self.outbox_drops),
            glyph_rate_drops: Self::n(&self.glyph_rate_drops),
            glyph_size_drops: Self::n(&self.glyph_size_drops),
            offers: Self::n(&self.offers),
            answers: Self::n(&self.answers),
            ice: Self::n(&self.ice),
            auth_fail: Self::n(&self.auth_fail),
        }
    }

    pub fn inc(a: &AtomicU64) {
        a.fetch_add(1, Ordering::Relaxed);
    }

    pub fn add(a: &AtomicU64, n: u64) {
        if n > 0 {
            a.fetch_add(n, Ordering::Relaxed);
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct MetricsSnap {
    pub joins_ok: u64,
    pub joins_fail: u64,
    pub leaves: u64,
    pub glyph_msgs: u64,
    pub hex_msgs: u64,
    pub chat_msgs: u64,
    pub outbox_drops: u64,
    pub glyph_rate_drops: u64,
    pub glyph_size_drops: u64,
    pub offers: u64,
    pub answers: u64,
    pub ice: u64,
    pub auth_fail: u64,
}
