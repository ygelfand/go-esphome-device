package esphomedevice

import "github.com/ygelfand/go-esphome-device/api"

// SendspinFeaturePairingToken is SendspinCapabilities.feature_flags' bit for a device that hands
// out its Sendspin pairing token with SendspinPairingTokenRequest.
const SendspinFeaturePairingToken = 1 << 0

// capabilities answers DeviceCapabilitiesRequest with what the device info also says, plus the
// Sendspin client, which only the capabilities carry.
func (c *Conn) capabilities() *api.DeviceCapabilitiesResponse {
	r := &api.DeviceCapabilitiesResponse{
		BluetoothProxy: &api.BluetoothProxyCapabilities{FeatureFlags: uint32(c.info.BluetoothFeatures)},
		VoiceAssistant: &api.VoiceAssistantCapabilities{FeatureFlags: uint32(c.info.VoiceFeatures)},
	}
	if c.info.Sendspin != nil {
		r.Sendspin = &api.SendspinCapabilities{
			FeatureFlags: SendspinFeaturePairingToken,
			ClientId:     c.info.Sendspin().ClientID,
		}
	}
	return r
}

// sendspinToken answers SendspinPairingTokenRequest. The token is a secret that lets whoever holds
// it pair with the device, so it is handed out only over a connection encrypted with a
// provisioned key.
func (c *Conn) sendspinToken() *api.SendspinPairingTokenResponse {
	if !c.Encrypted() {
		return &api.SendspinPairingTokenResponse{Status: api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_ENCRYPTION_REQUIRED}
	}
	p := c.info.Sendspin()
	switch {
	case p.Disabled:
		return &api.SendspinPairingTokenResponse{Status: api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_DISABLED}
	case p.Token == "":
		return &api.SendspinPairingTokenResponse{Status: api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_NOT_READY}
	}
	return &api.SendspinPairingTokenResponse{Status: api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_OK, Token: p.Token}
}
