
package obj

import (
	"fmt"
	"encoding/json"
)

func PrintAsJson(data interface{}) {
	json, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	fmt.Println(string(json))
}
func FindVal(intf interface{}, keylist... string) (res interface{}, ok bool) {
	res = intf
	if len(keylist) == 0 {
		ok = true
		return
	}
	for _, k := range keylist {
		var test bool
		var obj map[string]interface{}
		obj, test = res.(map[string]interface{})
		if (! test) {
			// data is not object
			ok = false
			return
		}

		res, test = obj[k]
		if (! test) {
			// key not exists
			ok = false
			return
		}
	}
	ok = true
	return
}
func FindFloat64(intf interface{}, keylist... string) (res float64, ok bool) {
	val, ok := FindVal(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.(float64)
	return
}
func FindString(intf interface{}, keylist... string) (res string, ok bool) {
	val, ok := FindVal(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.(string)
	return
}
func FindArray(intf interface{}, keylist... string) (res []interface{}, ok bool) {
	val, ok := FindVal(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.([]interface{})
	return
}

