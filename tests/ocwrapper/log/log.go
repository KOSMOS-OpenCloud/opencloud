package log

import "log"

func Println(message string) {
	log.Println("[ocwrapper]", message)
}

func Panic(err error) {
	log.Panic("[ocwrapper]", err.Error())
}

func Fatalln(err error) {
	log.Fatalln("[ocwrapper]", err.Error())
}
