//! E2E: two peers ↔ gy-sfu (media) — welcome/offer/answer + glyph & chat fan-out.
//!
//! ```bash
//! cargo test -p gy-sfu --features media --test e2e_media -- --nocapture
//! ```

use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde_json::{json, Value};
use tokio::sync::mpsc;
use tokio::time::{sleep, timeout};
use tokio_tungstenite::{connect_async, tungstenite::Message};
use uuid::Uuid;
use webrtc::api::interceptor_registry::register_default_interceptors;
use webrtc::api::media_engine::MediaEngine;
use webrtc::api::APIBuilder;
use webrtc::data_channel::data_channel_message::DataChannelMessage;
use webrtc::data_channel::RTCDataChannel;
use webrtc::ice_transport::ice_candidate::RTCIceCandidateInit;
use webrtc::ice_transport::ice_server::RTCIceServer;
use webrtc::peer_connection::configuration::RTCConfiguration;
use webrtc::peer_connection::sdp::session_description::RTCSessionDescription;
use webrtc::peer_connection::RTCPeerConnection;

struct SfuProc {
    child: Child,
    port: u16,
}

impl Drop for SfuProc {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn spawn_sfu() -> SfuProc {
    let port = 19000 + (std::process::id() % 1000) as u16;
    let mut candidates: Vec<String> = Vec::new();
    if let Ok(bin) = std::env::var("CARGO_BIN_EXE_gy-sfu") {
        candidates.push(bin);
    }
    candidates.extend([
        "target/release/gy-sfu".into(),
        "target/debug/gy-sfu".into(),
        "./target/release/gy-sfu".into(),
        "./target/debug/gy-sfu".into(),
    ]);

    let mut child = None;
    let mut used = String::new();
    for c in &candidates {
        let mut cmd = Command::new(c);
        cmd.arg("--bind")
            .arg(format!("127.0.0.1:{port}"))
            .stdout(Stdio::null())
            .stderr(Stdio::piped());
        match cmd.spawn() {
            Ok(ch) => {
                child = Some(ch);
                used = c.clone();
                break;
            }
            Err(_) => continue,
        }
    }
    let child = child.unwrap_or_else(|| {
        panic!("could not spawn gy-sfu from {candidates:?}. Build: cargo build --features media")
    });
    eprintln!("e2e: spawned {used} on :{port}");
    SfuProc { child, port }
}

async fn wait_health(port: u16) {
    let url = format!("http://127.0.0.1:{port}/health");
    for _ in 0..50 {
        if let Ok(body) = http_get(&url).await {
            if body.contains("\"ok\":true") || body.contains("\"ok\": true") {
                assert!(
                    body.contains("\"media\":true") || body.contains("\"media\": true"),
                    "SFU not media-enabled: {body}. Rebuild with --features media"
                );
                return;
            }
        }
        sleep(Duration::from_millis(100)).await;
    }
    panic!("SFU health timeout on :{port}");
}

async fn http_get(url: &str) -> Result<String, String> {
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    // url: http://127.0.0.1:PORT/health
    let rest = url
        .strip_prefix("http://")
        .ok_or_else(|| "bad url".to_string())?;
    let (hostport, path) = rest.split_once('/').unwrap_or((rest, ""));
    let path = format!("/{path}");
    let (host, port_s) = hostport.split_once(':').unwrap_or((hostport, "80"));
    let port: u16 = port_s.parse().unwrap_or(80);
    let mut stream = tokio::net::TcpStream::connect((host, port))
        .await
        .map_err(|e| e.to_string())?;
    let req = format!("GET {path} HTTP/1.1\r\nHost: {host}\r\nConnection: close\r\n\r\n");
    stream
        .write_all(req.as_bytes())
        .await
        .map_err(|e| e.to_string())?;
    let mut buf = Vec::new();
    stream
        .read_to_end(&mut buf)
        .await
        .map_err(|e| e.to_string())?;
    Ok(String::from_utf8_lossy(&buf).into_owned())
}

async fn build_pc() -> Arc<RTCPeerConnection> {
    let mut m = MediaEngine::default();
    m.register_default_codecs().unwrap();
    let mut registry = webrtc::interceptor::registry::Registry::new();
    registry = register_default_interceptors(registry, &mut m).unwrap();
    let api = APIBuilder::new()
        .with_media_engine(m)
        .with_interceptor_registry(registry)
        .build();
    let config = RTCConfiguration {
        ice_servers: vec![RTCIceServer {
            urls: vec!["stun:stun.l.google.com:19302".to_owned()],
            ..Default::default()
        }],
        ..Default::default()
    };
    Arc::new(api.new_peer_connection(config).await.unwrap())
}

struct PeerClient {
    nick: String,
    peer_id: Uuid,
    pc: Arc<RTCPeerConnection>,
    glyph_rx: mpsc::UnboundedReceiver<Value>,
    chat_rx: mpsc::UnboundedReceiver<String>,
    write: Arc<
        tokio::sync::Mutex<
            futures_util::stream::SplitSink<
                tokio_tungstenite::WebSocketStream<
                    tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>,
                >,
                Message,
            >,
        >,
    >,
    _bg: tokio::task::JoinHandle<()>,
}

impl PeerClient {
    async fn connect(port: u16, nick: &str, room: &str) -> Self {
        let url = format!("ws://127.0.0.1:{port}/ws?room={room}&nick={nick}");
        let (ws, _) = connect_async(&url).await.expect("ws connect");
        let (write, mut read) = ws.split();
        let write = Arc::new(tokio::sync::Mutex::new(write));
        let pc = build_pc().await;

        let (glyph_tx, glyph_rx) = mpsc::unbounded_channel::<Value>();
        let (chat_tx, chat_rx) = mpsc::unbounded_channel::<String>();
        let (welcome_tx, mut welcome_rx) = mpsc::unbounded_channel::<Uuid>();

        {
            let glyph_tx = glyph_tx.clone();
            let chat_tx = chat_tx.clone();
            let nick_c = nick.to_string();
            pc.on_data_channel(Box::new(move |dc: Arc<RTCDataChannel>| {
                let glyph_tx = glyph_tx.clone();
                let chat_tx = chat_tx.clone();
                let nick_c = nick_c.clone();
                Box::pin(async move {
                    let label = dc.label().to_ascii_lowercase();
                    eprintln!("{nick_c} got DC {label}");
                    dc.on_message(Box::new(move |msg: DataChannelMessage| {
                        let glyph_tx = glyph_tx.clone();
                        let chat_tx = chat_tx.clone();
                        let label = label.clone();
                        Box::pin(async move {
                            let raw = String::from_utf8_lossy(&msg.data).into_owned();
                            if label == "chat" {
                                if let Ok(v) = serde_json::from_str::<Value>(&raw) {
                                    if let Some(t) = v.get("text").and_then(|x| x.as_str()) {
                                        let _ = chat_tx.send(t.to_string());
                                        return;
                                    }
                                }
                                let _ = chat_tx.send(raw);
                            } else if let Ok(v) = serde_json::from_str::<Value>(&raw) {
                                let _ = glyph_tx.send(v);
                            }
                        })
                    }));
                })
            }));
        }

        let _ = pc.create_data_channel("glyph", None).await;
        let _ = pc.create_data_channel("chat", None).await;

        {
            let w = Arc::clone(&write);
            pc.on_ice_candidate(Box::new(move |c| {
                let w = Arc::clone(&w);
                Box::pin(async move {
                    let Some(c) = c else { return };
                    if let Ok(init) = c.to_json() {
                        let msg = json!({
                            "type": "ice",
                            "candidate": {
                                "candidate": init.candidate,
                                "sdpMid": init.sdp_mid,
                                "sdpMLineIndex": init.sdp_mline_index,
                            }
                        });
                        let mut g = w.lock().await;
                        let _ = g.send(Message::Text(msg.to_string().into())).await;
                    }
                })
            }));
        }

        let pc2 = Arc::clone(&pc);
        let write2 = Arc::clone(&write);
        let glyph_tx2 = glyph_tx.clone();
        let chat_tx2 = chat_tx.clone();
        let nick_owned = nick.to_string();
        let bg = tokio::spawn(async move {
            while let Some(Ok(msg)) = read.next().await {
                let text = match msg {
                    Message::Text(t) => t.to_string(),
                    Message::Binary(b) => String::from_utf8_lossy(&b).into_owned(),
                    Message::Close(_) => break,
                    _ => continue,
                };
                let v: Value = match serde_json::from_str(&text) {
                    Ok(v) => v,
                    Err(_) => continue,
                };
                let typ = v.get("type").and_then(|t| t.as_str()).unwrap_or("");
                match typ {
                    "welcome" => {
                        eprintln!("{nick_owned} welcome media={:?}", v.get("media"));
                        if let Some(id) = v.get("peer_id").and_then(|p| p.as_str()) {
                            if let Ok(u) = Uuid::parse_str(id) {
                                let _ = welcome_tx.send(u);
                            }
                        }
                    }
                    "answer" => {
                        if let Some(sdp) = v.get("sdp").and_then(|s| s.as_str()) {
                            let answer = RTCSessionDescription::answer(sdp.to_string()).unwrap();
                            if let Err(e) = pc2.set_remote_description(answer).await {
                                eprintln!("answer err: {e}");
                            } else {
                                eprintln!("{nick_owned} applied SFU answer");
                            }
                        }
                    }
                    "offer" => {
                        if let Some(sdp) = v.get("sdp").and_then(|s| s.as_str()) {
                            let offer = RTCSessionDescription::offer(sdp.to_string()).unwrap();
                            if pc2.set_remote_description(offer).await.is_ok() {
                                if let Ok(answer) = pc2.create_answer(None).await {
                                    let mut gather = pc2.gathering_complete_promise().await;
                                    let _ = pc2.set_local_description(answer).await;
                                    let _ = gather.recv().await;
                                    if let Some(local) = pc2.local_description().await {
                                        let msg = json!({"type":"answer","sdp": local.sdp});
                                        let mut w = write2.lock().await;
                                        let _ = w.send(Message::Text(msg.to_string().into())).await;
                                    }
                                }
                            }
                        }
                    }
                    "ice" => {
                        if let Some(c) = v.get("candidate") {
                            let cand = c.get("candidate").and_then(|x| x.as_str()).unwrap_or("");
                            if !cand.is_empty() {
                                let init = RTCIceCandidateInit {
                                    candidate: cand.to_string(),
                                    sdp_mid: c
                                        .get("sdpMid")
                                        .and_then(|x| x.as_str())
                                        .map(|s| s.to_string()),
                                    sdp_mline_index: c
                                        .get("sdpMLineIndex")
                                        .and_then(|x| x.as_u64())
                                        .map(|n| n as u16),
                                    username_fragment: None,
                                };
                                let _ = pc2.add_ice_candidate(init).await;
                            }
                        }
                    }
                    "glyph" => {
                        let _ = glyph_tx2.send(v);
                    }
                    "chat" => {
                        if let Some(t) = v.get("text").and_then(|x| x.as_str()) {
                            let _ = chat_tx2.send(t.to_string());
                        }
                    }
                    "error" => eprintln!("{nick_owned} sfu error: {v}"),
                    _ => {}
                }
            }
        });

        let peer_id = timeout(Duration::from_secs(5), welcome_rx.recv())
            .await
            .expect("welcome timeout")
            .expect("peer_id");

        let offer = pc.create_offer(None).await.unwrap();
        let mut gather = pc.gathering_complete_promise().await;
        pc.set_local_description(offer).await.unwrap();
        let _ = gather.recv().await;
        let local = pc.local_description().await.unwrap();
        {
            let mut w = write.lock().await;
            w.send(Message::Text(
                json!({"type":"offer","sdp": local.sdp}).to_string().into(),
            ))
            .await
            .unwrap();
        }

        for _ in 0..80 {
            if pc.remote_description().await.is_some() {
                break;
            }
            sleep(Duration::from_millis(50)).await;
        }
        sleep(Duration::from_millis(800)).await;

        Self {
            nick: nick.into(),
            peer_id,
            pc,
            glyph_rx,
            chat_rx,
            write,
            _bg: bg,
        }
    }

    async fn send_ws(&self, v: Value) {
        let mut w = self.write.lock().await;
        w.send(Message::Text(v.to_string().into())).await.unwrap();
    }

    async fn send_glyph_ws(&self, n: u32, data: Vec<u8>) {
        self.send_ws(json!({"type":"glyph","n": n, "data": data}))
            .await;
    }

    async fn send_chat_ws(&self, text: &str) {
        self.send_ws(json!({"type":"chat","text": text, "from": self.nick}))
            .await;
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn e2e_two_peer_glyph_and_chat_sync() {
    let sfu = spawn_sfu();
    wait_health(sfu.port).await;
    eprintln!("e2e: health ok media=true");

    let room = "e2e-dojo";
    let a = PeerClient::connect(sfu.port, "alice", room).await;
    let mut b = PeerClient::connect(sfu.port, "bob", room).await;
    eprintln!("e2e: peers {} / {}", a.peer_id, b.peer_id);

    a.send_chat_ws("hello-from-alice").await;
    let chat = timeout(Duration::from_secs(3), b.chat_rx.recv())
        .await
        .expect("chat timeout")
        .expect("chat msg");
    assert!(
        chat.contains("hello-from-alice"),
        "bob should receive chat, got {chat}"
    );
    eprintln!("e2e: chat OK ({chat})");

    let mut grid = vec![0u8; 25 * 25];
    for i in 0..grid.len() {
        grid[i] = (i % 200) as u8;
    }
    a.send_glyph_ws(25, grid).await;
    let glyph = timeout(Duration::from_secs(3), b.glyph_rx.recv())
        .await
        .expect("glyph timeout")
        .expect("glyph msg");
    let n = glyph.get("n").and_then(|x| x.as_u64()).unwrap_or(0);
    let data = glyph
        .get("data")
        .and_then(|d| d.as_array())
        .map(|a| a.len())
        .unwrap_or(0);
    assert_eq!(n, 25, "glyph n");
    assert_eq!(data, 625, "glyph data len");
    eprintln!("e2e: glyph OK n={n} len={data}");

    eprintln!(
        "e2e: alice pc={:?} bob pc={:?}",
        a.pc.connection_state(),
        b.pc.connection_state()
    );
    eprintln!("e2e: PASS glyph+chat sync (two peers via gy-sfu media)");
}
