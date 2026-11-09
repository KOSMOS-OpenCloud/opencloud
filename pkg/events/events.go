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

// DockNotification is emitted by OpenDock (dApps such as chat or tudu) for a persistent notification.
// Subject and message come ready-made from the dApp, so new dApps need no change here.
// MessageRich may use the placeholder {user} (the executant); Route is a web route opened on click.
type DockNotification struct {
	Executant   *user.UserId // who triggered the notification
	ReceiverIDs []string     // users to notify (user opaque ids)
	Plugin      string       // dApp plugin, e.g. "chat"
	Kind        string       // kind within the plugin, e.g. "message", "mention"
	ObjectID    string       // id of the object in the dApp (room, todo)
	Subject     string
	Message     string
	MessageRich string // optional, with {user}
	Route       string // optional web route, e.g. "/files/rooms/<id>"
	Timestamp   time.Time
}

func (DockNotification) Unmarshal(v []byte) (interface{}, error) {
	e := DockNotification{}
	err := json.Unmarshal(v, &e)
	return e, err
}
