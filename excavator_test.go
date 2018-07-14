package excavator_test

import (
	"log"
	"strings"
	"testing"

	"github.com/godcong/excavator"
)

func TestRadical_Iterator(t *testing.T) {
	log.SetFlags(log.Lshortfile)
	root := excavator.Self()
	radical := root.SelfRadical("耳")
	log.Println(radical)
	if radical == nil {
		return
	}
	c := radical.SelfCharacter("耿")
	log.Println(c)

}

func TestRadical_Add(t *testing.T) {
	text := "汉字五行：土　是否为常用字：否"
	s := strings.SplitAfter(text, "：")
	log.Println(s, len(s))

}
