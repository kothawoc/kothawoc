package main

import (
	"os"
	"time"

	"github.com/kothawoc/go-nntp"
	client "github.com/kothawoc/kothawoc"
)

const examplepost string = `From: <nobody@example.com>
Newsgroups: misc.test
Subject: Another Code test
Date: %s
Organization: spy internetworking

This is a test post.

that is longer now!
`

func main() {

	c := client.NewClient(os.Getenv("PWD")+"/data", 1119)

	//c.Dial()
	/*
		// Post an article
		err := c.NNTPclient.Post(strings.NewReader(fmt.Sprintf(examplepost, time.Now().UTC().Format(time.RFC1123Z))))
		if err != nil {
			log.Printf("FAILED posting [%v], reconnecting.", err)
			c.Dial()
		} else {
			log.Printf("Good Posted!\n")
		}

		// Post an article
		err = c.NNTPclient.Post(strings.NewReader(fmt.Sprintf(examplepost, time.Now().UTC().Format(time.RFC1123Z))))
		if err != nil {
			log.Printf("FAILED posting [%v]", err)
		} else {
			log.Printf("Good Posted!\n")
		}

		key, _ := c.ConfigGetGetBytes("deviceKey")
		//msg := client.MessageControl{}
		//msg.Try(key)

		h := messages.MessageTool{}
		h.Construct()
		h.Preamble = "bollox!"
		msg, err := h.Sign(key)
		//rawmesg := h.writeRaw(true)
		fmt.Printf("\n--------------------\nMessage:\n%s\n------ [ %#v ]-----------\n", msg, err)
		//crlfbreak := regexp.MustCompile("\r\n\r\n")
		fmt.Printf("Did it verify the signature? [%v]\n", h.Verify())
		body := strings.SplitN(msg, "\r\n\r\n", 2)[1]
		//body := crlfbreak.Split(msg, 2)[1]
		art := h.Article
		art.Body = strings.NewReader(body)
		h.Article = art

		//	h.ParseBody()
		//fmt.Printf("-------------\nBefore:\n%s\n--------\nAfter:\n%s\n-------------\n", rawmesg, h.writeRaw(true))
		fmt.Printf("Did it verify the signature? [%v]\n", h.Verify())
	*/

	c.CreateNewGroup("test.group.two", "this is a test group that should fail", nntp.PostingPermitted)

	for {
		<-time.After(time.Second)
	}
}
