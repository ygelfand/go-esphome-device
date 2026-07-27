package esphomedevice

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/ygelfand/go-esphome-device/api"
)

// MessageInfo describes one API message as declared in api.proto. ID is the value of
// the (id) option, which is what travels on the wire ahead of the payload.
type MessageInfo struct {
	ID     uint32
	Source api.APISourceType
	Name   protoreflect.FullName
	Type   protoreflect.MessageType
}

// FromDevice reports whether the device is the sender.
func (m MessageInfo) FromDevice() bool {
	return m.Source == api.APISourceType_SOURCE_SERVER || m.Source == api.APISourceType_SOURCE_BOTH
}

// FromClient reports whether Home Assistant is the sender.
func (m MessageInfo) FromClient() bool {
	return m.Source == api.APISourceType_SOURCE_CLIENT || m.Source == api.APISourceType_SOURCE_BOTH
}

var (
	messagesByID   = map[uint32]MessageInfo{}
	messagesByName = map[protoreflect.FullName]MessageInfo{}
)

func init() {
	msgs := api.File_api_proto.Messages()
	for i := 0; i < msgs.Len(); i++ {
		md := msgs.Get(i)

		opts, ok := md.Options().(*descriptorpb.MessageOptions)
		if !ok || !proto.HasExtension(opts, api.E_Id) {
			continue
		}
		id := proto.GetExtension(opts, api.E_Id).(uint32)
		if id == 0 {
			continue
		}

		mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
		if err != nil {
			continue
		}

		info := MessageInfo{
			ID:     id,
			Source: proto.GetExtension(opts, api.E_Source).(api.APISourceType),
			Name:   md.FullName(),
			Type:   mt,
		}
		messagesByID[info.ID] = info
		messagesByName[info.Name] = info
	}
}

func LookupID(id uint32) (MessageInfo, bool) {
	info, ok := messagesByID[id]
	return info, ok
}

func Lookup(m proto.Message) (MessageInfo, bool) {
	info, ok := messagesByName[m.ProtoReflect().Descriptor().FullName()]
	return info, ok
}

// NewMessage allocates an empty message for the given wire ID.
func NewMessage(id uint32) (proto.Message, bool) {
	info, ok := messagesByID[id]
	if !ok {
		return nil, false
	}
	return info.Type.New().Interface(), true
}

func Messages() []MessageInfo {
	out := make([]MessageInfo, 0, len(messagesByID))
	for _, info := range messagesByID {
		out = append(out, info)
	}
	return out
}
