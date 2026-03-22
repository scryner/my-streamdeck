# my-streamdeck

`my-streamdeck` is a macOS menu bar application for running a custom Stream Deck dashboard.
It turns a Stream Deck into a small desktop control surface with live widgets such as a clock,
calendar, system stats, network bandwidth, weather, caffeinate status, and QUI status.

## Overview

- Runs as a menu bar app on macOS
- Reads widget configuration from `~/.my-streamdeck/config.yaml`
- Can generate a starter config template with `init`
- Renders dynamic key images directly on the Stream Deck
- Supports clickable widgets for actions such as opening Calendar or external links

## Requirements

- macOS
- A supported Elgato Stream Deck Plus device
- Go 1.24+ for local development

## Usage

Run the application:

```bash
go run .
```

Generate a config template:

```bash
go run . init
```

This creates:

```text
~/.my-streamdeck/config.yaml.template
```

If `~/.my-streamdeck/config.yaml` does not exist, the app starts with a default widget set that
does not require external configuration.

## Configuration

Widgets are configured through YAML. A typical setup looks like this:

```yaml
widgets:
  - type: clock
    first: analog
  - type: calendar
  - type: sysstat
  - type: network
    interface: en0
  - type: weather.today
  - type: weather.forecast
  - type: caffeinate
  - type: qui
settings:
  - brightness: "100"
  - weather.location: Seoul
  - qui.base_url: https://qui.example.com
  - qui.access_token: INPUT-YOUR-QUI-ACCESS-TOKEN
```

## License

This project is released under the MIT License. See [LICENSE](LICENSE).
