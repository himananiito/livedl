package log4gui

import (
	"fmt"
	"encoding/json"
)

func print(k, v string) {
	bs, e := json.Marshal(map[string]string{
		k: v,
	})
	if(e != nil) {
		fmt.Println(e)
		return
	}
	fmt.Println("$" + string(bs) + "$")
}
func Info(s string) {
	print("Info", s)
}
func Error(s string) {
	print("Error", s)
}