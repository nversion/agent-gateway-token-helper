#!/usr/bin/env node
// Checks the setup generator in docs/index.html without a browser: loads the AGW module
// from the page, generates every file for a few configurations, and verifies that the
// output parses (JSON, plist via plutil where available, TOML shape, .reg shape).
// scripts/linting.sh runs it. Usage: node scripts/check-generator.mjs [--write <dir>]

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import vm from "node:vm";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const html = readFileSync(join(root, "docs/index.html"), "utf8");
const m = html.match(/<script>\n([\s\S]*?)\n<\/script>/);
if (!m) { throw new Error("no <script> block in docs/index.html"); }

// Run the page script with no document: only the AGW module is defined.
const sandbox = { module: { exports: {} }, console };
vm.runInNewContext(m[1], sandbox, { filename: "docs/index.html" });
const AGW = sandbox.module.exports;
if (!AGW || !AGW.cards) { throw new Error("AGW module not exported"); }

const base = {
    provider: "entra", gateway: "https://agw.example.com/", mcpUrl: "", tenant: "", hd: "", profile: "work",
    issuer: "", clientId: "", scopes: "openid email profile offline_access", redirectPort: "",
    winDir: "C:\\Program Files\\Agent Gateway helper\\", macDir: "/usr/local/bin/", linuxDir: "/opt/agw/bin",
    hive: "HKLM", regInts: "dword", ttl: "240", timeout: "300", org: "Contoso \"Quotes\" & Co", prefix: "com.contoso."
};
const configs = {
    entra: base,
    entraTenant: { ...base, tenant: "contoso.onmicrosoft.com", hive: "HKCU", regInts: "string", mcpUrl: "https://agw.example.com/mcp" },
    google: { ...base, provider: "google", hd: "example.com", ttl: "1800" },
    custom: { ...base, provider: "custom", issuer: "https://idp.example.com/realms/main", clientId: "agent-gateway", redirectPort: "8400+" }
};
const uuids = { profile: "11111111-2222-4333-8444-555555555555", payload: "66666666-7777-4888-9999-AAAAAAAAAAAA" };
const plutil = spawnSync("plutil", ["-help"]).status !== null;
const writeDir = process.argv.indexOf("--write") >= 0 ? process.argv[process.argv.indexOf("--write") + 1] : null;

let failures = 0, files = 0;
const fail = (msg) => { failures++; console.error("FAIL " + msg); };
const expect = (cond, msg) => { if (!cond) { fail(msg); } };

// A badly formed configuration must be reported, not silently generated.
const bad = AGW.validate(AGW.derive({ ...base, gateway: "http://plain", ttl: "0", timeout: "900", winDir: "relative" }));
expect(bad.problems.length >= 4, "validation should report four problems, got " + JSON.stringify(bad.problems));

for (const [name, form] of Object.entries(configs)) {
    const c = AGW.derive(form);
    const v = AGW.validate(c);
    expect(v.problems.length === 0, name + ": unexpected problems " + JSON.stringify(v.problems));
    const cards = AGW.cards(c, uuids);
    const seen = new Set();
    for (const k of cards) {
        files++;
        const id = `${name}/${k.harness}/${k.os}/${k.file}`;
        expect(!seen.has(id), id + ": duplicate card"); seen.add(id);
        expect(typeof k.content === "string" && k.content.length > 0, id + ": empty content");
        expect(k.content.endsWith("\n"), id + ": no trailing newline");
        expect(Array.isArray(k.steps) && k.steps.length > 0 && k.steps.every((s) => typeof s === "string"), id + ": steps");
        expect(!/undefined|NaN|\[object/.test(k.content), id + ": placeholder leaked into output");
        const hl = AGW.highlight(k.content, AGW.langOf(k.file));
        expect(hl.replace(/<span class="tk-[a-z]+">|<\/span>/g, "") === AGW.xml(k.content), id + ": highlighting changed the text");
        if (writeDir) {
            const dir = join(writeDir, name, k.harness, k.os);
            mkdirSync(dir, { recursive: true });
            writeFileSync(join(dir, k.file), k.content);
        }
        switch (k.file.split(".").pop()) {
            case "json": {
                let o;
                try { o = JSON.parse(k.content); } catch (e) { fail(id + ": invalid JSON: " + e.message); break; }
                if (k.file === "managed-settings.json") {
                    expect(o.inferenceCredentialHelper === c.path.linux, id + ": helper path");
                    expect(typeof o.inferenceCredentialHelperTtlSec === "number", id + ": Linux integers are numbers");
                }
                if (k.file === "agw-token.json") {
                    const p = o.profiles[c.provider === "custom" ? c.profile : "default"];
                    expect(p, id + ": profile missing");
                    if (c.ttl >= 300) { expect(p["min-valid"] === "31m", id + ": min-valid, got " + p["min-valid"]); }
                }
                if (k.file === "settings.json") { expect(o.env.CLAUDE_CODE_API_KEY_HELPER_TTL_MS === String(c.ttl * 1000), id + ": TTL ms"); }
                if (k.file === ".mcp.json") { expect(/--output headers$/.test(o.mcpServers.gateway.headersHelper), id + ": headersHelper"); }
                break;
            }
            case "reg": {
                expect(k.content.startsWith("Windows Registry Editor Version 5.00\r\n\r\n["), id + ": header");
                expect(!/[^\r]\n/.test(k.content), id + ": must be CRLF throughout");
                expect(k.content.includes(`[${c.hive === "HKLM" ? "HKEY_LOCAL_MACHINE" : "HKEY_CURRENT_USER"}\\SOFTWARE\\Policies\\Claude]`), id + ": hive");
                expect(k.content.includes('"inferenceCredentialHelperWindows"="C:\\\\Program Files\\\\Agent Gateway helper\\\\' + c.binary + '.exe"'), id + ": escaped path");
                const ttlLine = c.regInts === "dword" ? '"inferenceCredentialHelperTtlSec"=dword:' + c.ttl.toString(16).padStart(8, "0") : `"inferenceCredentialHelperTtlSec"="${c.ttl}"`;
                expect(k.content.includes(ttlLine + "\r\n"), id + ": TTL line, wanted " + ttlLine);
                if (c.args.length) { expect(k.content.includes('"inferenceCredentialHelperArgs"="[\\"--profile\\",\\"work\\"]"'), id + ": args encoding"); }
                break;
            }
            case "plist":
            case "mobileconfig": {
                expect(k.content.includes("<key>inferenceCredentialHelper</key>\n" + (k.file.endsWith("plist") ? "\t" : "\t\t\t") + "<string>" + c.path.macos + "</string>"), id + ": helper path");
                expect(!k.content.includes("<key>inferenceCredentialHelperWindows</key>"), id + ": Windows key in macOS file");
                expect(k.content.includes("<string>" + c.ttl + "</string>"), id + ": TTL as string");
                if (k.file.endsWith("mobileconfig")) {
                    expect(k.content.includes("<string>com.anthropic.claudefordesktop</string>"), id + ": PayloadType");
                    expect(k.content.includes("&quot;Quotes&quot; &amp; Co"), id + ": XML escaping");
                }
                if (plutil) {
                    const r = spawnSync("plutil", ["-lint", "-s", "-"], { input: k.content });
                    expect(r.status === 0, id + ": plutil -lint: " + String(r.stderr || r.stdout));
                }
                break;
            }
            case "toml": {
                const lines = k.content.split("\n");
                expect(lines[0] === 'model_provider = "gateway"', id + ": first line");
                expect(k.content.includes("base_url = \"https://agw.example.com/v1\""), id + ": base_url");
                const cmd = lines.find((l) => l.startsWith("command = "));
                if (k.os === "windows") { expect(cmd === "command = 'C:\\Program Files\\Agent Gateway helper\\" + c.namedBinary + ".exe'", id + ": windows command " + cmd); }
                else { expect(cmd === `command = "${c.namedPath[k.os]}"`, id + ": command " + cmd); }
                expect(k.content.includes("refresh_interval_ms = " + c.ttl * 1000), id + ": refresh interval");
                expect(k.content.includes("timeout_ms = " + c.timeout * 1000), id + ": timeout");
                break;
            }
            case "sh":
            case "cmd":
                expect(k.snippet === true, id + ": commands are a snippet");
                // Paths with spaces are quoted; plain Unix paths are left as they are.
                expect(k.content.includes("\n" + (k.os === "windows" ? '"' + c.namedPath.windows + '"' : c.namedPath[k.os]) + "\n"), id + ": bare command quoting");
                expect(k.content.includes("--output headers\n"), id + ": headers command");
                expect(k.content.includes(" login" + (c.args.length ? " " + c.args.join(" ") : "") + "\n"), id + ": login command before flags");
                break;
            default:
                fail(id + ": unknown file type");
        }
    }
    const harnesses = new Set(cards.map((k) => k.harness));
    for (const h of ["desktop", "code", "codex", "other"]) { expect(harnesses.has(h), name + ": no cards for " + h); }
    expect(harnesses.has("json") === AGW.needsConfigFile(c), name + ": agw-token.json presence");
    for (const os of ["windows", "macos", "linux"]) { expect(cards.some((k) => k.harness === "desktop" && k.os === os), name + ": no Claude Desktop file for " + os); }
}

// Spot checks on the highlighter.
expect(AGW.highlight('{"a": 1}', "json") === '{<span class="tk-key">&quot;a&quot;</span>: <span class="tk-num">1</span>}', "json highlighting");
expect(AGW.highlight("<b>x</b>", "") === "&lt;b&gt;x&lt;/b&gt;", "unknown language is escaped");

console.log(`${files} generated files checked across ${Object.keys(configs).length} configurations` + (plutil ? " (plist linted with plutil)" : " (plutil not available, plist lint skipped)"));
if (failures) { console.error(failures + " check(s) failed"); process.exit(1); }
