package esphomedevice

import (
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// Camera is a still picture Home Assistant asks for, rather than a state the device pushes.
type Camera struct {
	Base

	// Image is asked for a JPEG when Home Assistant wants one. It runs on the connection's
	// goroutine, so a camera that takes its time holds that connection up.
	Image func() ([]byte, error)
}

// chunk is how much of a picture goes in one message. The noise transport writes its frame length
// in sixteen bits, so nothing can reach 64KB, and a still off a real sensor is several times that.
const chunk = 32 << 10

func (c *Camera) describe() proto.Message {
	return &api.ListEntitiesCameraResponse{
		ObjectId:          c.ObjectID,
		Key:               c.Key(),
		Name:              c.Name,
		Icon:              c.Icon,
		EntityCategory:    c.Category,
		DisabledByDefault: c.DisabledByDefault,
		DeviceId:          c.DeviceID,
	}
}

// state is nil, the way Button's is: there is nothing to send until a picture is asked for, and
// subscribing must not cost one.
func (c *Camera) state() proto.Message { return nil }

// send writes one picture, in as many messages as it takes. Only the last says done, which is
// what tells Home Assistant it has the whole thing.
func (c *Camera) send(conn *Conn) error {
	if c.Image == nil {
		return nil
	}

	img, err := c.Image()
	if err != nil {
		return err
	}

	for at := 0; ; {
		end := min(at+chunk, len(img))
		last := end == len(img)

		if err := conn.Send(&api.CameraImageResponse{
			Key:      c.Key(),
			Data:     img[at:end],
			Done:     last,
			DeviceId: c.DeviceID,
		}); err != nil {
			return err
		}
		if last {
			return nil
		}
		at = end
	}
}
