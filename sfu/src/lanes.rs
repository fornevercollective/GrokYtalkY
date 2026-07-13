//! Stream lanes — keep terminals on glyph/hex; never force full HD into ▀.

/// Well-known lane identifiers for hybrid DOJO + Cloudflare delivery.
pub const GLYPH: &str = "glyph";
pub const HEX: &str = "hex";
pub const MID: &str = "mid";
pub const FULL: &str = "full";

pub const ALL: &[&str] = &[GLYPH, HEX, MID, FULL];

/// Default lanes for terminal / Glyph peers (tight aesthetic).
pub fn default_dojo_lanes() -> Vec<String> {
    vec![GLYPH.to_string(), HEX.to_string()]
}

/// True if lane is safe for half-block / Glyph Matrix consumers.
#[allow(dead_code)] // used by media path / future hub bridge
pub fn is_low_res(lane: &str) -> bool {
    matches!(lane, GLYPH | HEX)
}

/// Filter client-requested lanes to known set.
pub fn normalize(lanes: &[String]) -> Vec<String> {
    let mut out = Vec::new();
    for l in lanes {
        let t = l.trim().to_ascii_lowercase();
        if ALL.contains(&t.as_str()) && !out.iter().any(|x| x == &t) {
            out.push(t);
        }
    }
    if out.is_empty() {
        return default_dojo_lanes();
    }
    out
}
