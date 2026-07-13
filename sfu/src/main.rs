//! gy-sfu — minimal DOJO WebRTC SFU sidecar for GrokYtalkY.
//!
//! Default: signaling + rooms + glyph/hex/chat WS fan-out.
//! `--features media`: webrtc-rs PeerConnections, track fan-out, DataChannels.

mod lanes;
#[cfg(feature = "media")]
mod media;
mod room;
mod signaling;

use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::State;
use axum::routing::get;
use axum::{Json, Router};
use clap::Parser;
use tower_http::cors::CorsLayer;
use tower_http::trace::TraceLayer;
use tracing_subscriber::EnvFilter;

use room::RoomRegistry;
use signaling::ws_handler;

#[derive(Parser, Debug)]
#[command(name = "gy-sfu", about = "GrokYtalkY DOJO SFU sidecar")]
struct Args {
    /// Listen address
    #[arg(long, default_value = "127.0.0.1:9880")]
    bind: String,

    /// Soft cap peers per room (DOJO jam target ~32; node can go higher)
    #[arg(long, default_value_t = 64)]
    max_peers_per_room: usize,

    /// Optional GrokYtalkY hub URL to bridge later (glyph/hex from mesh)
    #[arg(long, default_value = "")]
    hub: String,
}

#[derive(Clone)]
pub struct AppState {
    pub rooms: Arc<RoomRegistry>,
    pub max_peers_per_room: usize,
    pub hub: String,
    pub media_enabled: bool,
    #[cfg(feature = "media")]
    pub media: Option<Arc<media::MediaHub>>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env().add_directive("gy_sfu=info".parse()?))
        .init();

    let args = Args::parse();
    let media_enabled = cfg!(feature = "media");

    #[cfg(feature = "media")]
    let media = {
        match media::MediaHub::new().await {
            Ok(m) => {
                tracing::info!("media hub ready (webrtc-rs · STUN + optional GY_SFU_TURN_URLS)");
                Some(m)
            }
            Err(e) => {
                tracing::error!("media hub failed: {e}");
                None
            }
        }
    };

    let state = AppState {
        rooms: Arc::new(RoomRegistry::new()),
        max_peers_per_room: args.max_peers_per_room,
        hub: args.hub,
        media_enabled: media_enabled && {
            #[cfg(feature = "media")]
            {
                media.is_some()
            }
            #[cfg(not(feature = "media"))]
            {
                false
            }
        },
        #[cfg(feature = "media")]
        media,
    };

    let app = Router::new()
        .route("/health", get(health))
        .route("/rooms", get(list_rooms))
        .route("/ws", get(ws_handler))
        .layer(CorsLayer::permissive())
        .layer(TraceLayer::new_for_http())
        .with_state(state);

    let addr: SocketAddr = args.bind.parse()?;
    tracing::info!(
        %addr,
        media = media_enabled,
        "gy-sfu listening (signaling{}; hybrid: CF for 1k+ viewers)",
        if media_enabled {
            "+media"
        } else {
            "-only"
        }
    );

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn health(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(serde_json::json!({
        "ok": true,
        "service": "gy-sfu",
        "media": st.media_enabled,
        "rooms": st.rooms.room_count(),
        "peers": st.rooms.peer_count(),
        "max_peers_per_room": st.max_peers_per_room,
        "hub": if st.hub.is_empty() { serde_json::Value::Null } else { st.hub.clone().into() },
        "lanes": lanes::ALL,
        "turn": std::env::var("GY_SFU_TURN_URLS").ok().map(|_| true).unwrap_or(false),
    }))
}

async fn list_rooms(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(serde_json::json!({ "rooms": st.rooms.snapshot() }))
}
