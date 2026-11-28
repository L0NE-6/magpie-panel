import assert from "node:assert/strict"
import { test } from "node:test"
import { imageURLAllowed } from "./pi.js"

// Where an image block's url may point (#8047): Bun's fetch reads file: and
// speaks to whatever a url names, so the plugin host asks this before it
// fetches, as the Go gateway's own fetchers already do.
test("imageURLAllowed takes http and https", () => {
  assert.equal(imageURLAllowed("https://example.com/a.png"), true)
  assert.equal(imageURLAllowed("http://cdn.example.com/x.jpg"), true)
  assert.equal(imageURLAllowed("http://172.32.0.1/a.png"), true)
  assert.equal(imageURLAllowed("http://[2606:4700::1111]/a.png"), true)
})

test("imageURLAllowed refuses a scheme that is not http or https", () => {
  assert.equal(imageURLAllowed("file:///etc/passwd"), false)
  assert.equal(imageURLAllowed("ftp://example.com/a.png"), false)
  assert.equal(imageURLAllowed("data:image/png;base64,AAAA"), false)
  assert.equal(imageURLAllowed("not a url"), false)
  assert.equal(imageURLAllowed(""), false)
})

test("imageURLAllowed refuses this machine and this network", () => {
  for (const url of [
    "http://127.0.0.1/a.png",
    "http://localhost/a.png",
    "http://169.254.169.254/latest/meta-data/",
    "http://10.1.2.3/a.png",
    "http://192.168.1.5/a.png",
    "http://172.16.0.9/a.png",
    "http://100.64.0.1/a.png",
    "http://[::1]/a.png",
    "http://[fd00::1]/a.png",
    "http://[fe80::1]/a.png",
    "http://printer.local/a.png",
    "http://metadata.internal/a.png",
    "http://intranet/a.png",
  ]) {
    assert.equal(imageURLAllowed(url), false, url)
  }
})
