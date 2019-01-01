package nicocasprop

type broadcaster struct {
	BroadcasterPageURL   string `json:"broadcasterPageUrl"`
	ID                   string `json:"id"`
	IsBroadcaster        bool   `json:"isBroadcaster"`
	IsClosedBataTestUser bool   `json:"isClosedBataTestUser"`
	IsOperator           bool   `json:"isOperator"`
	Level                int64  `json:"level"`
	Nickname             string `json:"nickname"`
	PageURL              string `json:"pageUrl"` // "http://www.nicovideo.jp/user/XXXX"
}
type community struct {
	ID string `json:"id"`
}
type propsNicoCas struct {
	Broadcaster broadcaster `json:"broadcaster"`
	Community   community   `json:"community"`
}

type program struct {
	NicoliveProgramID string `json:"nicoliveProgramId"`
	Title             string `json:"title"`
}

type NicocasProperty struct {
	Broadcaster broadcaster `json:"broadcaster"`
	Program     program     `json:"program"`
}

func (p NicocasProperty) GetID() string {
	return p.Program.NicoliveProgramID
}

func (p NicocasProperty) GetName() string {
	return p.Broadcaster.Nickname
}

func (p NicocasProperty) GetTitle() string {
	return p.Program.Title
}
