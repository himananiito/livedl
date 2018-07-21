package cryptoconf

import (
	"golang.org/x/crypto/sha3"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"io/ioutil"
	"os"
	"encoding/json"
	"fmt"
	"log"
)

func Set(dataSet map[string]string, fileName, pass string) (err error) {
	var data map[string]interface{}
	if _, test := os.Stat(fileName); test == nil {
		data, err = Load(fileName, pass)
		if err != nil {
			return
		}
	} else {
		data = map[string]interface{}{}
	}
	for key, val := range dataSet {
		data[key] = val
	}

	digest := sha3.Sum256([]byte(pass))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		log.Fatalln(err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Fatalln(err.Error())
	}

	nonceSize := aesgcm.NonceSize()
	// Never use more than 2^32 random nonces with a given key because of the risk of a repeat.
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		log.Fatalln(err.Error())
	}

	plaintext, err := json.Marshal(data)
	if err != nil {
		return
	}
	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)
	//fmt.Printf("%#v\n", ciphertext)

	file, err := os.Create(fileName)
	if err != nil {
		return
	}
	defer file.Close()
	if _, err = file.Write(ciphertext); err != nil {
		return
	}

	return
}

func Load(file, pass string) (data map[string]interface{}, err error) {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		err = nil
		return
	}

	digest := sha3.Sum256([]byte(pass))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		log.Fatalln(err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Fatalln(err.Error())
	}

	nonceSize := aesgcm.NonceSize()

	nonce, ciphertext := b[:nonceSize], b[nonceSize:]

	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		err = fmt.Errorf("Password wrong for config: %s", file)
		return
	}

	////fmt.Printf("%s\n", plaintext)
	data = map[string]interface{}{}
	err = json.Unmarshal(plaintext, &data)

	return
}