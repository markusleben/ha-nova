# Rust vs Go for Relay CLI — Research Spec

Date: 2026-03-13
Status: Research complete

## Context

Tiny cross-platform CLI tool (~500 LOC) that:
1. Reads auth token from OS credential store (macOS Keychain, Windows Credential Manager, Linux Secret Service)
2. Makes HTTP requests with auth headers
3. Filters JSON output using jq-compatible syntax

Team is TypeScript/Bash-native, not systems programmers.

---

## 1. Rust Equivalent Libraries

### HTTP Client: `ureq` (recommended) vs `reqwest`

**`ureq`** — clear winner for this use case.
- **Blocking/synchronous** by design — no async runtime needed
- Pure Rust, `forbid(unsafe_code)`
- Default TLS via `rustls` (no OpenSSL dependency)
- Supports cookies, JSON, HTTP proxies, SOCKS4/5, gzip, brotli
- Minimal dependency tree compared to `reqwest`
- Perfect for a simple "make HTTP request, get response" CLI

**`reqwest`** — overkill here.
- Async-first (requires tokio runtime = +2-3 MB binary overhead)
- Has a blocking wrapper, but it still pulls in tokio
- Better for high-concurrency apps, not simple CLIs

**Verdict:** `ureq` is the right choice. Simpler API, smaller binary, no async complexity.

### JSON + jq Filtering: `jaq`

**`jaq`** (v2.3, July 2025) — excellent.
- Rust reimplementation of jq, usable as a library (`jaq-core` crate)
- Fastest on 23/36 benchmarks vs jq-1.8.1 and gojq-0.12.17
- Supports YAML, CBOR, TOML, XML beyond JSON
- Thread-safe, supports arbitrary data types beyond JSON
- Professional security audit by Radically Open Security
- 500+ test suite, high jq compatibility
- Can embed directly — no shelling out to `jq`

**Go alternative:** `gojq` exists but is slower than `jaq` in benchmarks. Also usable as a library.

**Verdict:** `jaq-core` as a library is a strong advantage for Rust. Embed jq filtering without external dependencies.

### Keyring: `keyring` crate (v4.0.0-rc.3)

**Platform support:**
- macOS/iOS: native Keychain via `apple-native` feature
- Windows: native Credential Store via `windows-native` feature
- Linux: `keyutils` (native) OR DBus Secret Service (sync/async)
- FreeBSD/OpenBSD: DBus Secret Service

**Maturity:** 3,800+ dependents, 709 stars, 51 releases. API stabilizing (RC status as of Feb 2026).

**Caveats:**
- Linux sync-secret-service requires DBus (and optionally OpenSSL), but can be statically linked via `vendored` feature
- Thread safety warning: concurrent access to same credential can fail on Windows/Linux

**Go alternative:** `zalando/go-keyring` — similar coverage but fewer features, less actively maintained.

**Verdict:** Both languages have good keyring support. Rust's `keyring` crate is more feature-rich and more actively maintained.

---

## 2. Binary Size Comparison

### Rust (with full optimization)

**Cargo.toml profile for minimum size:**
```toml
[profile.release]
opt-level = "z"        # optimize for size
lto = "fat"            # whole-program LTO
codegen-units = 1      # single codegen unit
panic = "abort"        # no unwind tables
strip = "symbols"      # strip debug symbols
```

**Expected sizes for a CLI with HTTP + JSON + keyring:**

| Configuration | Estimated Size |
|---|---|
| `cargo build --release` (defaults) | 4-6 MB |
| + `strip = "symbols"` | 3-4 MB |
| + `opt-level = "z"` + `lto = "fat"` + `codegen-units = 1` + `panic = "abort"` | 1.5-2.5 MB |
| + UPX compression | 600 KB - 1 MB |

**Key factor:** Using `ureq` with `rustls` (default) avoids pulling in OpenSSL, keeping the binary lean. Using `reqwest` with tokio would add 2-3 MB.

### Go

**Expected sizes for equivalent CLI:**

| Configuration | Estimated Size |
|---|---|
| `go build` (defaults) | 8-12 MB |
| + `go build -ldflags="-s -w"` (strip) | 5-8 MB |
| + UPX compression | 2-3 MB |

**Why Go binaries are larger:**
- Go runtime (goroutine scheduler, GC) is always included (~2-3 MB baseline)
- Standard library is statically linked
- Reflection metadata for JSON handling

### Comparison

| | Rust (optimized) | Go (stripped) |
|---|---|---|
| Without UPX | 1.5-2.5 MB | 5-8 MB |
| With UPX | 600 KB - 1 MB | 2-3 MB |
| Ratio | 1x | ~3x larger |

**Verdict:** Rust wins clearly on binary size. 3-4x smaller is significant for a CLI tool that ships as a single binary to end users.

---

## 3. Cross-Compilation

### Rust

**Native cross-compilation (from macOS):**
- macOS ARM -> macOS x86: works out of the box
- macOS -> Linux (musl): `rustup target add x86_64-unknown-linux-musl` — works but needs a C linker for some crates
- macOS -> Windows: needs `x86_64-pc-windows-gnu` target + MinGW linker, or MSVC libs

**`cargo-zigbuild`** (recommended approach):
- Uses Zig as a cross-compilation linker
- `cargo zigbuild --target x86_64-unknown-linux-gnu` from macOS — just works
- Handles all the C toolchain headaches transparently
- Single `cargo install cargo-zigbuild` + Zig binary

**`cross`** (Docker-based):
- Builds inside Docker containers with pre-configured toolchains
- Most reliable but requires Docker running
- Slower than native builds

**Keyring crate cross-compilation issues:**
- macOS Keychain uses Security framework (Apple-only) — cannot cross-compile FROM Linux TO macOS
- Windows Credential Manager uses Win32 APIs — cannot cross-compile FROM macOS without Windows SDK
- Linux Secret Service needs DBus headers — but `vendored` feature can statically link them
- **Practical impact:** You likely need CI (GitHub Actions) with runners per OS for release builds. This is standard practice and not Rust-specific.

### Go

**Native cross-compilation:**
- `GOOS=windows GOARCH=amd64 go build` — just works for pure Go code
- No extra toolchains, no Docker, no Zig
- **BUT:** CGO complicates things. If your keyring library uses CGO (many do), you need C cross-compilers, same as Rust

**`go-keyring` CGO status:**
- macOS: uses CGO (calls Security framework via cgo)
- Windows: pure Go (uses `syscall`)
- Linux: uses DBus (pure Go implementation available via `godbus`)
- **Practical impact:** CGO on macOS breaks the "just set GOOS" story

### Comparison

| | Rust | Go |
|---|---|---|
| Pure code cross-compile | Needs linker setup | `GOOS=x GOARCH=y` just works |
| With keyring (native OS APIs) | CI per platform needed | CI per platform needed (CGO on macOS) |
| Tooling | cargo-zigbuild / cross | Built-in (when no CGO) |

**Verdict:** Go has simpler cross-compilation for pure Go code. But once you need native OS keyring APIs, both languages require per-platform CI builds. This is a wash for the actual use case.

---

## 4. Developer Experience

### Learning Curve

**For a TypeScript/Bash team:**
- **Go:** 1-2 weeks to be productive. Syntax is simple, error handling is explicit but repetitive (`if err != nil`). Feels like "typed Python with curly braces."
- **Rust:** 3-6 weeks to be productive. Ownership/borrowing model is the main hurdle. The compiler fights you hard initially, then becomes your best friend. String handling (`String` vs `&str`) is confusing at first.

**For a 500-line CLI:**
- Go: a developer can write this on day 2
- Rust: a developer can write this in week 2-3 (first project)

### Compile Times

**For a ~500 LOC project with ~10 dependencies:**

| | Rust | Go |
|---|---|---|
| Clean build | 15-45 seconds | 3-8 seconds |
| Incremental build | 2-5 seconds | 1-3 seconds |
| `cargo check` (type-check only) | 1-3 seconds | N/A (Go is always fast) |

Rust compile times are a non-issue for a project this small. They become painful at 50K+ LOC with heavy macro usage (not your case).

### Dependency Stability

**Rust (Cargo/crates.io):**
- Semver is strictly enforced by Cargo
- `Cargo.lock` pins exact versions
- Breaking changes are rare in mature crates (`ureq`, `serde`, `clap`)
- `keyring` is still RC (v4.0.0-rc.3) — some API churn possible
- Ecosystem is very stable overall; "dependency hell" is rare

**Go (Go Modules):**
- Also strict semver via Go Modules
- Similar stability story
- Slightly fewer choices (smaller ecosystem for niche crates)

### Error Messages

- **Rust:** Best-in-class compiler error messages. The borrow checker errors have improved dramatically — they now suggest fixes.
- **Go:** Simple, direct error messages. Less to go wrong.

---

## 5. Real Downsides of Rust

### When Rust is a BAD choice here:

1. **Team velocity risk:** If the team needs to ship in days, not weeks, Rust's learning curve is a real cost. The first Rust project always takes 2-3x longer than expected.

2. **Maintenance burden:** If multiple team members need to modify this CLI and none know Rust, every change becomes a learning exercise. Go is "boring" enough that anyone can jump in.

3. **Over-engineering signal:** For a 500-line CLI, Rust's safety guarantees (memory safety, thread safety) solve problems you don't have. This tool is not a server, not concurrent, not processing untrusted input at scale.

4. **`keyring` crate is still RC:** v4.0.0-rc.3 means the API might shift. In Go, `go-keyring` is more stable (though less feature-rich).

5. **Async contamination risk:** If a future requirement needs async (WebSocket, streaming), Rust async is significantly harder than Go goroutines. Go's concurrency model is simpler by an order of magnitude.

### When Go clearly wins over Rust:

| Scenario | Why Go Wins |
|---|---|
| Team knows neither language | Go is learnable in days, Rust in weeks |
| Multiple developers maintain it | Go's simplicity = lower bus factor risk |
| Rapid prototyping needed | Go compiles faster, fewer type gymnastics |
| Future WebSocket/streaming needs | Goroutines are trivial; Rust async is hard |
| Cross-compile without CI | `GOOS=x go build` (when no CGO) |

### When Rust clearly wins over Go:

| Scenario | Why Rust Wins |
|---|---|
| Binary size matters | 1.5-2.5 MB vs 5-8 MB |
| Single developer maintains it long-term | Compiler catches bugs Go's type system misses |
| jq filtering is core feature | `jaq-core` is best-in-class, embeddable |
| Distribution to end users | Smaller download, no runtime dependencies |
| You want to learn Rust | This is actually a perfect first Rust project |

---

## 6. Recommendation

### For HA NOVA specifically:

**Go is the pragmatic choice.** Here is why:

1. The team is TS/Bash-native. Go's learning curve is dramatically lower.
2. The CLI is ~500 LOC — Rust's safety guarantees are not solving real problems at this scale.
3. Multiple contributors may need to touch this. Go's simplicity reduces bus factor.
4. Binary size difference (2.5 MB vs 6 MB) matters but is not critical for a CLI tool users install once.
5. Cross-compilation is simpler when CGO can be avoided (possible with pure-Go keyring on Linux/Windows, only macOS needs CGO).

**Rust is the ambitious choice.** Consider it if:

1. Binary size is a hard requirement (shipping in HA app store, bandwidth-constrained updates).
2. The jq filtering needs to be deeply integrated (jaq-core as library is genuinely better than any Go equivalent).
3. A single developer will own this long-term and wants the compiler as a safety net.
4. The team wants to invest in Rust skills for future projects.

### Bottom line:

For a TS/Bash team building a 500-line CLI — **Go gets you there in 1/3 the time with 90% of the benefits.** Rust gives you a smaller, faster binary with better jq integration, but the development cost is real and the benefits are marginal for this specific use case.

If binary size or jq embedding quality tips the scales, Rust is viable — but go in with eyes open about the 2-3 week learning investment.
