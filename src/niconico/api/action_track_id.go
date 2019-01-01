package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"../../httpcommon"
)

type metaInt struct {
	Status int `json:"status"`
}
type actionTrackID struct {
	Meta metaInt `json:"meta"`
	Data string  `json:"data"`
}

func GetActionTrackID(ctx context.Context) (id string, err error) {

	req, err := http.NewRequest("POST", "https://public.api.nicovideo.jp/v1/action-track-ids.json", nil)
	if err != nil {
		return
	}

	req = req.WithContext(ctx)

	client := httpcommon.GetClient()
	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}

	fmt.Println(string(bs))
	var atid actionTrackID
	json.Unmarshal(bs, &atid)

	if atid.Data != "" {
		id = atid.Data
	} else {
		err = fmt.Errorf("action-track-id not found")
		return
	}
	return
}
