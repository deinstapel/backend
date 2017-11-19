package qbin

import (
	"crypto/rand"
	"errors"
	"io/ioutil"
	"math/big"
	"strings"
)

var words = []string{}
var characters = strings.Split("abcdefghijklmnopqrstuvwxyz0123456789", "")

func randomWord(array []string, try int, err error) string {
	if try > 10 {
		// We somehow tried 10 times to get a random value and it failed every time. Something is extremely wrong here.
		Log.Criticalf("crypto/rand issue - something is probably extremely wrong! %s", err)
		panic(err)
	}

	i, err := rand.Int(rand.Reader, big.NewInt(int64(len(array))))
	if err != nil {
		return randomWord(array, try+1, err)
	}
	return array[i.Int64()]
}

// LoadWordsFile imports the word definition file used for names.
func LoadWordsFile(filename string) error {
	content, err := ioutil.ReadFile(filename)
	if err == nil {
		source := strings.Split(string(content), "\n")
		words = make([]string, 0)
		for _, word := range source {
			word = strings.ToLower(strings.TrimSpace(word))
			if len(word) > 0 && !strings.HasPrefix(word, "#") {
				words = append(words, word)
			}
		}
		if len(words) == 0 {
			return errors.New("file doesn't contain any words")
		}
		Log.Debugf("%d words loaded.", len(words))
	}
	return err
}

// GenerateName generates a slug in the format "cornflake-peddling-bp0q". Note that this function will return an empty string if an error occurs along the way!
func GenerateName() string {
	text := ""
	var word string
	for i := 0; i < 6; i++ {
		if i < 2 && len(words) > 0 {
			word = randomWord(words, 0, nil)
		} else {
			word = randomWord(characters, 0, nil)
		}

		if i < 3 && len(words) > 0 {
			text += "-" + word
		} else {
			text += word
		}
	}

	return strings.TrimPrefix(text, "-")
}
