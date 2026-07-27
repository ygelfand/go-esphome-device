# go-esphome-device

A Go library for implementing the **device side** of the ESPHome native API — write a
program that Home Assistant talks to as if it were an ESPHome device.

Home Assistant's built-in `esphome` integration connects to you, enumerates your entities and
drives them, so there's no custom integration for users to install.

Connections are encrypted with a Noise PSK.

> Status: early. Not yet usable.

## Usage

_TODO once the API settles._

## Protocol version

`proto/api.proto` comes from upstream ESPHome; `proto/ESPHOME_VERSION` records which release
the generated bindings track. To move to a newer one:

```sh
make proto REF=2026.7.0
```

## Testing

`cmd/sim_voice_assistant` is a simulated voice satellite. Point a real Home Assistant at it to
check protocol behaviour without hardware:

```sh
make sim
```

## License

MIT. `api.proto` is redistributed from ESPHome, whose dual license applies GPLv3 only to
`.c`, `.cpp`, `.h`, `.hpp`, `.tcc` and `.ino` files, and MIT to everything else.
