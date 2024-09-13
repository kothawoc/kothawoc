package messages

import (
	"bufio"
	"bytes"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cretz/bine/torutil/ed25519"
	"github.com/kothawoc/go-nntp"
	"github.com/kothawoc/kothawoc/pkg/keytool"
	serr "github.com/kothawoc/kothawoc/pkg/serror"
)

/*

From: "example.* Administrator" <admin@noc.example>
         Newsgroups: example.admin.info
         Date: 27 Feb 2002 12:50:22 +0200
         Subject: cmsg newgroup example.admin.info moderated
         Approved: admin@noc.example
         Control: newgroup example.admin.info moderated
         Message-ID: <ng-example.admin.info-20020227@noc.example>
         MIME-Version: 1.0
         Content-Type: multipart/mixed; boundary="nxtprt"
         Content-Transfer-Encoding: 8bit

		 This is a MIME control message.
         --nxtprt
         Content-Type: application/news-groupinfo; charset=us-ascii

         For your newsgroups file:
         example.admin.info      About the example.* groups (Moderated)

         --nxtprt
         Content-Type: text/plain; charset=us-ascii

         A moderated newsgroup for announcements about new newsgroups in
         the example.* hierarchy.

         --nxtprt--

*/

const SignatureHeader string = "X-Kothawoc-Signature"

var SignatureFields []string = []string{
	"From",
	"Newsgroups",
	"Date",
	"Subject",
	"Approved",
	"Control",
	"Distribution",
	"Message-Id",
	"Supersedes",
	"Sender",
	"Mime-Version",
	"Content-Type",
	"Content-Transfer-Encoding",
}

type MimePart struct {
	Header  textproto.MIMEHeader
	Content []byte
}

type MessageTool struct {
	Parts    []MimePart
	Preamble string
	Article  *nntp.Article
}

func (m *MessageTool) Sign(myKey keytool.EasyEdKey) (string, error) {
	//func (m *MessageTool) Sign(privateKey ed25519.PrivateKey) (string, error) {
	pubKey, err := myKey.TorPubKey()
	if err != nil {
		return "", serr.New(err)
	}
	(*m).Article.Header.Set("Approved", hex.EncodeToString(pubKey))
	//(*m).Article.Header.Set("Approved", hex.EncodeToString(privateKey.PublicKey()))
	//myKey := keytool.EasyEdKey{}
	//myKey.SetTorPrivateKey(privateKey)
	torId, _ := myKey.TorId()
	(*m).Article.Header.Set("From", torId)
	data := m.writeRaw(true)
	msg, err := myKey.TorSign([]byte(data))
	if err != nil {
		return string(msg), serr.New(err)
	}
	signature := base32.StdEncoding.EncodeToString(msg)

	os.WriteFile("sign.txt", []byte(data), 0600)
	slog.Info("Signing message", "data", string(data), "signature", signature)
	(*m).Article.Header.Set(SignatureHeader, signature)
	return m.writeRaw(false), nil
}

func (m *MessageTool) RawMail() string {
	return m.writeRaw(false)
}

func (m *MessageTool) Verify() bool {
	//sigPart := m.Parts[len(m.Parts)-1]
	slog.Info("verify", "message", m.writeRaw(true))

	b32Signature := []byte(m.Article.Header.Get(SignatureHeader))

	dst := make([]byte, 256)
	n, err := base32.StdEncoding.Decode(dst, b32Signature)
	if err != nil {
		slog.Info("Failed to decode b32 signature.")
		return false
	}
	signature := dst[:n]

	pubKey, err := hex.DecodeString(m.Article.Header.Get("Approved"))
	if err != nil {
		slog.Info("Failed to decode pubkey.")
		return false
	}
	slog.Info("Checking Approved", "pubKey", pubKey, "b32Signature", b32Signature)
	os.WriteFile("verify.txt", []byte(m.writeRaw(true)), 0600)
	verified := ed25519.Verify(ed25519.PublicKey(pubKey), []byte(m.writeRaw(true)), signature)
	//verified = true
	return verified
}

func (m *MessageTool) writeRaw(signing bool) string {
	slog.Info("Write raw start and preamble", "signing", signing, "preamble", m.Preamble)
	// Buffer to store the email
	var buf bytes.Buffer

	// Create a multipart writer
	var writer *multipart.Writer
	//writer := buf
	_, params, err := mime.ParseMediaType(m.Article.Header.Get("Content-Type"))
	if err != nil {
		slog.Info("Errored in parsing media type for stuff", "error", err, "header", m.Article.Header.Get("Content-Type"))
		//		return ""
	} else {
		writer = multipart.NewWriter(&buf)
		writer.SetBoundary(params["boundary"])
	}

	for _, headerName := range SignatureFields {
		for _, value := range m.Article.Header.Values(headerName) {
			fmt.Fprintf(&buf, "%s: %s\r\n", headerName, value)
		}
	}

	// write rest of headers
	if !signing {

		sigFields := make([]string, len(SignatureFields))

		// Iterate over the original slice and convert each string to lowercase
		for i, s := range SignatureFields {
			sigFields[i] = strings.ToLower(s)
		}

		for key, values := range m.Article.Header {
			for _, value := range values {
				if !slices.Contains(sigFields, strings.ToLower(key)) {
					fmt.Fprintf(&buf, "%s: %s\r\n", key, value)
				}
			}
		}
	}
	fmt.Fprintf(&buf, "\r\n")
	m.Preamble = strings.Replace(string(m.Preamble), "\r", "", -1)
	m.Preamble = strings.Replace(string(m.Preamble), "\n", "\r\n", -1)
	fmt.Fprintf(&buf, m.Preamble)
	if len(m.Parts) > 1 {
		fmt.Fprintf(&buf, "\r\n")

		for _, part := range m.Parts {
			partWriter, err := writer.CreatePart(part.Header)
			if err != nil {
				//	log.Fatal(err)
				slog.Info("ERROR: writeRaw partWriter error", "error", err)
				return ""
			}
			part.Content = []byte(strings.Replace(string(part.Content), "\r", "", -1))
			partWriter.Write([]byte(strings.Replace(string(part.Content), "\n", "\r\n", -1)))
		}

		// Close the writer to finalize the email
		writer.Close()
	}

	return buf.String()
}

func (m *MessageTool) ParseBody() {

	hasMime := false
	_, params, err := mime.ParseMediaType(m.Article.Header.Get("Content-Type"))
	if err == nil {
		hasMime = true
		slog.Debug("have mime")
	}

	// Read the preamble manually
	reader := bufio.NewReader(m.Article.Body)
	var preamble bytes.Buffer

	parts := []MimePart{}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 1 && line[len(line)-1] == '\n' {
			/////		//	line[len(line)-1] = '\r'
			//		line = line[:len(line)-2] + "\r\n"
			/////		//log.Fatal("Delete it")
			/////		//line = line[:len(line)-1]
			/////		//		log.Fatal("Delete it", line[:len(line)-2], line[:len(line)-1])

			//if line == "\n" {
			//	line = "\r\n"
			//	line = line[:len(line)-1] //+ "\r\n"
			//log.Fatalf("Delete it [%v]", line[:len(line)-1])
			//	log.Fatalf("Delete it [%c]", line[len(line)-1])
		}
		if err == io.EOF {
			preamble.WriteString(line)

			//	fmt.Println("READ 1 Preamble:")
			//	fmt.Println(preamble.String())
			(*m).Preamble = preamble.String()
			return
		}
		if err != nil {
			preamble.WriteString(line)
			//	fmt.Println("READ 2 Preamble:")
			//	fmt.Println(preamble.String())
			m.Preamble = preamble.String()
			return
		}
		if hasMime && strings.HasPrefix(line, "--"+params["boundary"]) {
			reader = bufio.NewReader(io.MultiReader(strings.NewReader(line), reader))
			// Print the preamble
			//	fmt.Println("PREAD 3 reamble:")
			//	fmt.Println(preamble.String())
			m.Preamble = preamble.String()
			if m.Preamble[len(m.Preamble)-1] == '\n' {
				m.Preamble = m.Preamble[:len(m.Preamble)-1]
				//		fmt.Printf("LOLZ 3 READ 2 Preamble: [%s] ||lolz", m.Preamble)
			}

			//fmt.Println("LOLZ READ 2 Preamble:||", m.Preamble, "||lolz")

			// Now read the MIME parts
			mr := multipart.NewReader(reader, params["boundary"])
			//mr.

			for {
				part, err := mr.NextPart()
				if err == io.EOF {

					//	fmt.Printf("PREAD 4 parts reamble: [%#v][%s]", parts, parts)
					m.Parts = parts
					return
				}
				if err != nil {
					//	fmt.Printf("PREAD 5 parts reamble: [%v]", parts)

					m.Parts = parts
					return
				}

				//	fmt.Printf("Part Content-Type: %s\n", part.Header.Get("Content-Type"))
				//	if part.FileName() != "" {
				//		fmt.Printf("Attachment Filename: %s\n", part.FileName())
				//	}
				buf := new(bytes.Buffer)
				buf.ReadFrom(part)
				//	fmt.Printf("Part Content:\n%s\n\n", buf.String())

				parts = append(parts, MimePart{Header: part.Header, Content: buf.Bytes()})

			}

			//break
		}
		preamble.WriteString(line)
	}

}

func NewMessageTool() *MessageTool {
	return &MessageTool{
		Article: &nntp.Article{
			Header: textproto.MIMEHeader{
				"Date": {time.Now().UTC().Format(time.RFC1123Z)}},
		},
		Preamble: "",
		Parts:    []MimePart{},
	}
}

func NewMessageToolFromArticle(article *nntp.Article) *MessageTool {
	mt := &MessageTool{
		Article:  article,
		Preamble: "",
		Parts:    []MimePart{},
	}
	mt.ParseBody()
	return mt
}

func (m *MessageTool) ExampleMessageTemplate() *MessageTool {
	return &MessageTool{
		Article: &nntp.Article{
			Header: textproto.MIMEHeader{
				"Date":                      {time.Now().UTC().Format(time.RFC1123Z)},
				"Content-Type":              {"multipart/mixed; boundary=\"nxtprt\""},
				"Content-Transfer-Encoding": {"8bit"},
			},
		},
		Preamble: "This is a multipart mime message",
		Parts: []MimePart{
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"application/news-groupinfo; charset=us-ascii"}},
				Content: []byte("This is a magic bit of content\n\nCool eh?\n"),
			},
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=us-ascii"}},
				Content: []byte("A quick brown fox\njumps over the lazy\ndog.\n"),
			},
		},
	} //.Sign(ed25519.PrivateKey([]]bytes{"PrivateKey"})
}

func (m *MessageTool) Construct() {

	header := textproto.MIMEHeader{}
	header.Add("From", "sakflasfsdf")
	header.Add("Newsgroups", "alt.misc.test")
	header.Add("Date", time.Now().UTC().Format(time.RFC1123Z))
	header.Add("Subject", "Test Subject")
	header.Add("Approved", "by me")
	header.Add("Control", "a dodgy command")
	header.Add("Message-Id", "<12345-random-yourmum@her.house>")

	header.Add("MIME-Version", "1.0")
	header.Add("Content-Type", "multipart/mixed; boundary=\"nxtprt\"")
	header.Add("Content-Transfer-Encoding", "8bit")

	*m = MessageTool{
		Article: &nntp.Article{
			Header: header,
		},
		Preamble: "total bollox!\n\nyourmum\n",

		Parts: []MimePart{

			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"application/news-groupinfo; charset=us-ascii"}},
				Content: []byte("This is a magic bit of content\n\nCool eh?\n"),
			},
			{
				Header:  textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=us-ascii"}},
				Content: []byte("A quick brown fox\njumps over the lazy\ndog.\n"),
			},
		},
	}

}
