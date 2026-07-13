//! Media path (webrtc-rs) — enabled with `--features media`.
//!
//! Scaffold only: PeerConnection factory + notes for track/DataChannel forwarding.
//! Signaling works without this module; wire tracks here next.

#![cfg(feature = "media")]

use webrtc::api::interceptor_registry::register_default_interceptors;
use webrtc::api::media_engine::MediaEngine;
use webrtc::api::APIBuilder;
use webrtc::interceptor::registry::Registry;
use webrtc::peer_connection::configuration::RTCConfiguration;

/// Build a default WebRTC API (Opus + VP8/H264 if available in webrtc-rs build).
pub async fn build_api() -> Result<webrtc::api::API, webrtc::Error> {
    let mut m = MediaEngine::default();
    m.register_default_codecs()?;
    let mut registry = Registry::new();
    registry = register_default_interceptors(registry, &mut m)?;
    Ok(APIBuilder::new()
        .with_media_engine(m)
        .with_interceptor_registry(registry)
        .build())
}

pub fn default_config() -> RTCConfiguration {
    RTCConfiguration::default()
}

// Next steps (DOJO SFU):
// 1. Per-peer PeerConnection on offer/answer
// 2. OnTrack → fan-out to other peers' PeerConnections (true SFU)
// 3. DataChannel "glyph" / "hex" for low-res lanes (prefer over media tracks)
// 4. TURN from env: GY_SFU_TURN_URLS
