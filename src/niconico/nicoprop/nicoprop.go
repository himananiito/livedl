package nicoprop

import "fmt"

type stream struct {
	MaxQuality string `json:"maxQuality"` // normal high
}
type supplier struct {
	Name    string `json:"name"`
	PageURL string `json:"pageUrl"` // http://www.nicovideo.jp/user/XXXX
}
type program struct {
	NicoliveProgramID string   `json:"nicoliveProgramId"`
	ProviderType      string   `json:"providerType"`
	Status            string   `json:"status"` // ON_AIR
	Stream            stream   `json:"stream"`
	Supplier          supplier `json:"supplier"`
	Title             string   `json:"title"`
}

type socialGroup struct {
	CompanyName string `json:"companyName"` // 株式会社 ドワンゴ
	ID          string `json:"id"`          // コミュID
	Name        string `json:"name"`        // コミュ名
	Type        string `json:"type"`        // "channel"
}
type community struct {
	ID string `json:"id"`
}
type channel struct {
	ID string `json:"id"`
}
type relive struct {
	WebSocketUrl string `json:"webSocketUrl"`
}
type site struct {
	Relive relive `json:"relive"`
}
type NicoProperty struct {
	Channel     channel     `json:"channel"`
	Community   community   `json:"community"`
	Program     program     `json:"program"`
	SocialGroup socialGroup `json:"socialGroup"`
	Site        site        `json:"site"`
}

func (p NicoProperty) GetID() string {
	return p.Program.NicoliveProgramID
}

func (p NicoProperty) GetName() string {
	return fmt.Sprintf("%s／%s", p.SocialGroup.Name, p.Program.Supplier.Name)
}

func (p NicoProperty) GetTitle() string {
	return p.Program.Title
}
