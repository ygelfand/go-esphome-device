package esphomedevice

import "github.com/ygelfand/go-esphome-device/api"

// SendspinFeaturePairingToken is the SendspinCapabilities feature bit for SendspinPairingTokenRequest.
const SendspinFeaturePairingToken = 1 << 0

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

func (c *Conn) sendspinToken() *api.SendspinPairingTokenResponse {
	if !c.Encrypted() {
		return &api.SendspinPairingTokenResponse{
			Status: api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_ENCRYPTION_REQUIRED,
		}
	}
	p := c.info.Sendspin()
	if p.Token != "" {
		return &api.SendspinPairingTokenResponse{
			Status: api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_OK,
			Token:  p.Token,
		}
	}
	status := p.Status
	if status == api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_OK {
		status = api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_NOT_READY
	}
	return &api.SendspinPairingTokenResponse{Status: status}
}
