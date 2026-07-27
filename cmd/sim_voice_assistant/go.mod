module github.com/ygelfand/go-esphome-device/cmd/sim_voice_assistant

go 1.26

require (
	github.com/ebitengine/oto/v3 v3.4.0
	github.com/hajimehoshi/go-mp3 v0.3.4
	github.com/jonchammer/audio-io v0.3.0
	github.com/ygelfand/go-esphome-device v0.0.0
	golang.org/x/term v0.45.0
)

require (
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/libp2p/zeroconf/v2 v2.2.0 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/tools v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/ygelfand/go-esphome-device => ../..
