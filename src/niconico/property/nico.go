package nicoprop

type socialGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}
type nicoProperty struct {
	SocialGroup socialGroup `json:"socialGroup"`
}
