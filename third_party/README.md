# Third-Party Forks

This directory contains local forks of upstream dependencies that are built into
`my-streamdeck` via `replace` directives in the root `go.mod`.

## Why This Exists

The Stream Deck wake/restart leak investigation showed that the upstream
libraries did not always unblock a blocked HID input read during device close on
macOS. That left old listener goroutines alive after wake-triggered restarts.

To patch and verify the behavior locally without waiting on an upstream release,
the project uses checked-in copies of the affected modules.

## Active Replacements

- `rafaelmartins.com/p/streamdeck` -> `./third_party/streamdeck`
- `rafaelmartins.com/p/usbhid` -> `./third_party/usbhid`

See [go.mod](../go.mod).

## Upstream Sources

- `streamdeck`
  - Module path: `rafaelmartins.com/p/streamdeck`
  - Upstream docs: <https://rafaelmartins.com/p/streamdeck/>
  - License: BSD 3-Clause
- `usbhid`
  - Module path: `rafaelmartins.com/p/usbhid`
  - Upstream docs: <https://rafaelmartins.com/p/usbhid/>
  - License: BSD 3-Clause

The original license files are preserved in each fork directory.

## Local Changes

Current local modifications are intentionally small and focused on the wake
restart leak fix:

- `streamdeck/device.go`
  - Treats device-close `ErrDeviceIsClosed` during shutdown as a clean listener
    exit.
- `usbhid/device_darwin.go`
  - Adds explicit close signaling so blocked `GetInputReport()` calls wake up
    when the device is closed.
  - Serializes repeated close calls and preserves cleanup state while the
    runloop is being stopped.

## Maintenance Notes

- Prefer minimizing drift from upstream.
- If upstream accepts or releases an equivalent fix, remove the local fork and
  the `replace` directives.
- If the fork grows beyond a few targeted patches, move it to a dedicated fork
  repository and pin to that module source explicitly.
