package events

import (
	"encoding/json"
	"time"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

type ResourceMention struct {
	Executant *user.UserId
	UserIDs   []*user.UserId
	Ref       *provider.Reference
	Timestamp time.Time
}

func (ResourceMention) Unmarshal(v []byte) (interface{}, error) {
	e := ResourceMention{}
	err := json.Unmarshal(v, &e)
	return e, err
}

// TodoUpdate is emitted by external todo services (e.g. tudu) when a todo
// changes — delegation, date change, or completion. The userlog service
// picks it up and creates a persistent notification for the affected user.
type TodoUpdate struct {
	Executant  *user.UserId // who performed the action
	ReceiverID string       // user who should be notified
	TodoID     string       // external todo identifier
	Subject    string       // todo subject (short text)
	Action     string       // "delegated", "date_changed", "completed", "reopened"
	Comment    string       // delegation comment / reason (optional)
	Ref        *provider.Reference
	Timestamp  time.Time
}

func (TodoUpdate) Unmarshal(v []byte) (interface{}, error) {
	e := TodoUpdate{}
	err := json.Unmarshal(v, &e)
	return e, err
}
