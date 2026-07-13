//! gy-sfu — minimal DOJO WebRTC SFU sidecar for GrokYtalkY.
//!
//! Default: signaling + rooms + glyph/hex/chat WS fan-out.
//! `--features media`: webrtc-rs PeerConnections, track fan-out, DataChannels.

mod lanes;
mod metrics;
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

use metrics::Metrics;
use room::RoomRegistry;
use signaling::ws_handler;

#[derive(Parser, Debug)]
#[command(name = "gy-sfu", about = "GrokYtalkY DOJO SFU sidecar (jam-scale hardened)")]
struct Args {
    /// Listen address
    #[arg(long, default_value = "127.0.0.1:9880")]
    bind: String,

    /// Soft cap peers per room (DOJO jam target ~32; node can go higher)
    #[arg(long, default_value_t = 64, env = "GY_SFU_MAX_PEERS")]
    max_peers_per_room: usize,

    /// Soft cap total peers on this node (0 = unlimited)
    #[arg(long, default_value_t = 256, env = "GY_SFU_MAX_PEERS_NODE")]
    max_peers_node: usize,

    /// Per-peer WS outbox capacity (glyph/hex drop when full)
    #[arg(long, default_value_t = 64, env = "GY_SFU_OUTBOX")]
    outbox_capacity: usize,

    /// Max glyph payload bytes (n×n luminance)
    #[arg(long, default_value_t = 49 * 49, env = "GY_SFU_MAX_GLYPH_BYTES")]
    max_glyph_bytes: usize,

    /// Max hex payload string bytes
    #[arg(long, default_value_t = 16_384, env = "GY_SFU_MAX_HEX_BYTES")]
    max_hex_bytes: usize,

    /// Min interval between glyph frames per peer (ms); 0 = unlimited
    #[arg(long, default_value_t = 33, env = "GY_SFU_GLYPH_MIN_MS")]
    glyph_min_interval_ms: u64,

    /// Optional GrokYtalkY hub URL (informational; use `gy sfu-bridge` for live glyph bridge)
    #[arg(long, default_value = "")]
    hub: String,

    /// Shared room join token (empty = open DOJO). Env: GY_SFU_TOKEN
    #[arg(long, default_value = "", env = "GY_SFU_TOKEN")]
    token: String,

    /// Extra STUN URLs (comma-separated). Default Google STUN if empty.
    #[arg(long, default_value = "", env = "GY_SFU_STUN_URLS")]
    stun_urls: String,

    /// TURN servers: url,user,pass;url2,user2,pass2  (also GY_SFU_TURN_URLS)
    #[arg(long, default_value = "", env = "GY_SFU_TURN_URLS")]
    turn_urls: String,
}

#[derive(Clone)]
pub struct AppState {
    pub rooms: Arc<RoomRegistry>,
    pub metrics: Arc<Metrics>,
    pub max_peers_per_room: usize,
    pub max_peers_node: usize,
    pub outbox_capacity: usize,
    pub max_glyph_bytes: usize,
    pub max_hex_bytes: usize,
    pub glyph_min_interval_ms: u64,
    pub hub: String,
    /// Empty string = no auth required
    pub token: String,
    pub media_enabled: bool,
    pub ice_summary: IceSummary,
    #[cfg(feature = "media")]
    pub media: Option<Arc<media::MediaHub>>,
}

#[derive(Clone, Debug, Default)]
pub struct IceSummary {
    pub stun: Vec<String>,
    pub turn: bool,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env().add_directive("gy_sfu=info".parse()?))
        .init();

    let args = Args::parse();
    let media_enabled = cfg!(feature = "media");
    let metrics = Metrics::new();

    // Prefer CLI TURN; fall back to env already bound by clap env
    if !args.turn_urls.is_empty() {
        std::env::set_var("GY_SFU_TURN_URLS", &args.turn_urls);
    }
    if !args.stun_urls.is_empty() {
        std::env::set_var("GY_SFU_STUN_URLS", &args.stun_urls);
    }

    let ice_summary = IceSummary {
        stun: ice_stun_list(),
        turn: !std::env::var("GY_SFU_TURN_URLS")
            .unwrap_or_default()
            .is_empty(),
    };

    #[cfg(feature = "media")]
    let media = {
        match media::MediaHub::new().await {
            Ok(m) => {
                tracing::info!(
                    stun = ?ice_summary.stun,
                    turn = ice_summary.turn,
                    "media hub ready (webrtc-rs ICE)"
                );
                Some(m)
            }
            Err(e) => {
                tracing::error!("media hub failed: {e}");
                None
            }
        }
    };

    if !args.token.is_empty() {
        tracing::info!("auth token required (query ?token= or join.token)");
    }
    if ice_summary.turn {
        tracing::info!("TURN configured via GY_SFU_TURN_URLS / --turn-urls");
    } else {
        tracing::info!("TURN not set — LAN/STUN only; set GY_SFU_TURN_URLS for NAT jam peers");
    }

    let state = AppState {
        rooms: Arc::new(RoomRegistry::new()),
        metrics,
        max_peers_per_room: args.max_peers_per_room.max(1),
        max_peers_node: args.max_peers_node,
        outbox_capacity: args.outbox_capacity.max(8),
        max_glyph_bytes: args.max_glyph_bytes.max(169),
        max_hex_bytes: args.max_hex_bytes.max(256),
        glyph_min_interval_ms: args.glyph_min_interval_ms,
        hub: args.hub,
        token: args.token,
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
        ice_summary,
        #[cfg(feature = "media")]
        media,
    };

    let app = Router::new()
        .route("/health", get(health))
        .route("/metrics", get(metrics_handler))
        .route("/rooms", get(list_rooms))
        .route("/ws", get(ws_handler))
        .layer(CorsLayer::permissive())
        .layer(TraceLayer::new_for_http())
        .with_state(state);

    let addr: SocketAddr = args.bind.parse()?;
    tracing::info!(
        %addr,
        media = media_enabled,
        max_room = args.max_peers_per_room,
        max_node = args.max_peers_node,
        outbox = args.outbox_capacity,
        "gy-sfu listening (signaling{}; jam-scale; CF mid-lane for 1k+)",
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

fn ice_stun_list() -> Vec<String> {
    if let Ok(raw) = std::env::var("GY_SFU_STUN_URLS") {
        let v: Vec<String> = raw
            .split(',')
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .map(|s| s.to_string())
            .collect();
        if !v.is_empty() {
            return v;
        }
    }
    vec!["stun:stun.l.google.com:19302".into()]
}

async fn health(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(serde_json::json!({
        "ok": true,
        "service": "gy-sfu",
        "media": st.media_enabled,
        "rooms": st.rooms.room_count(),
        "peers": st.rooms.peer_count(),
        "max_peers_per_room": st.max_peers_per_room,
        "max_peers_node": st.max_peers_node,
        "outbox_capacity": st.outbox_capacity,
        "hub": if st.hub.is_empty() { serde_json::Value::Null } else { st.hub.clone().into() },
        "lanes": lanes::ALL,
        "stun": st.ice_summary.stun,
        "turn": st.ice_summary.turn,
        "auth": !st.token.is_empty(),
        "metrics": st.metrics.snap(),
    }))
}

async fn metrics_handler(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(serde_json::json!({
        "service": "gy-sfu",
        "rooms": st.rooms.snapshot(),
        "peers": st.rooms.peer_count(),
        "counters": st.metrics.snap(),
    }))
}

async fn list_rooms(State(st): State<AppState>) -> Json<serde_json::Value> {
    Json(serde_json::json!({
        "rooms": st.rooms.snapshot(),
        "max_peers_per_room": st.max_peers_per_room,
        "peers": st.rooms.peer_count(),
    }))
}
