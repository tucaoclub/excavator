package excavator

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-xorm/xorm"
	"github.com/gocolly/colly"
	"github.com/godcong/go-trait"
)

var log = trait.NewZapSugar()

const tmpFile = "tmp"

// Step ...
type Step int

// excavator run step status ...
const (
	StepAll       Step = 0
	StepRadical        = iota
	StepCharacter      = iota
)

// Excavator ...
type Excavator struct {
	Workspace string `json:"workspace"`
	URL       string `json:"url"`
	HTML      string `json:"html"`
	Radicals  map[string]Radical
	header    http.Header
	db        *xorm.Engine
	radical   chan *RadicalCharacter
	step      Step
	limit     int64
	character chan *Character
	selenium  *Selenium
}

// Limit ...
func (exc *Excavator) Limit() int64 {
	return exc.limit
}

// SetLimit ...
func (exc *Excavator) SetLimit(limit int64) {
	exc.limit = limit
}

// DB ...
func (exc *Excavator) DB() *xorm.Engine {
	return exc.db
}

// SetDB ...
func (exc *Excavator) SetDB(db *xorm.Engine) {
	exc.db = db
}

// Header ...
func (exc *Excavator) Header() http.Header {
	if exc.header == nil {
		return make(http.Header)
	}
	return exc.header
}

// SetHeader ...
func (exc *Excavator) SetHeader(header http.Header) {
	exc.header = header
}

// New ...
func New(url string, workspace string) *Excavator {
	log.With("url", url, "workspace", workspace).Info("init")
	return &Excavator{URL: url, Workspace: workspace, limit: 50}
}

// PreRun ...
func (exc *Excavator) PreRun() {
	if exc.db == nil {
		exc.db = InitSqlite3("exc.db")
	}
	e := exc.db.Sync2(RadicalCharacter{})
	if e != nil {
		panic(e)
	}
	e = exc.db.Sync2(Character{})
	if e != nil {
		panic(e)
	}
	exc.radical = make(chan *RadicalCharacter)
	exc.character = make(chan *Character)
	exc.selenium = NewSelenium("", 9515)
	exc.selenium.Start()
}

// Radical ...
func (exc *Excavator) Radical() (rc <-chan *RadicalCharacter) {
	if exc.step == StepRadical {
		return exc.radical
	}
	return nil
}

// Character ...
func (exc *Excavator) Character() (rc <-chan *Character) {
	if exc.step == StepCharacter {
		return exc.character
	}
	return nil
}

// Run ...
func (exc *Excavator) Run() error {
	log.Info("excavator run")
	exc.PreRun()
	switch exc.step {
	case StepAll:
		//go exc.parseRadical(exc.radical)
		//go exc.parseCharacter(exc.radical, exc.character)
	case StepRadical:
		go exc.parseRadical(exc.radical)
	case StepCharacter:
		go exc.findRadical(exc.radical)
		go exc.parseCharacter(exc.radical, exc.character)
	}

	return nil
}
func (exc *Excavator) findRadical(characters chan<- *RadicalCharacter) {
	defer func() {
		characters <- nil
	}()
	i, e := exc.db.Count(RadicalCharacter{})
	if e != nil || i == 0 {
		log.Error(e)
		return
	}
	for x := int64(0); x < i; x += exc.Limit() {
		rc := new([]RadicalCharacter)
		e := exc.db.Limit(int(exc.Limit()), int(x)).Find(&rc)
		if e != nil {
			log.Error(e)
			continue
		}
		for i := range *rc {
			characters <- &(*rc)[i]
		}
	}
}

func (exc *Excavator) parseRadical(characters chan<- *RadicalCharacter) {
	defer func() {
		characters <- nil
	}()
	c := colly.NewCollector()
	c.OnHTML("a[href][data-action]", func(element *colly.HTMLElement) {
		da := element.Attr("data-action")
		log.With("value", da).Info("data action")
		if da == "" {
			return
		}
		r, e := exc.parseAJAX(exc.URL, strings.NewReader(fmt.Sprintf("wd=%s", da)))
		if e != nil {
			return
		}
		for _, tmp := range *(*[]RadicalUnion)(r) {
			for i := range tmp.RadicalCharacterArray {
				rc := tmp.RadicalCharacterArray[i]
				e := exc.saveRadicalCharacter(&tmp.RadicalCharacterArray[i])
				if e != nil {
					log.Error(e)
					continue
				}
				characters <- &rc
			}
		}
		log.With("value", r).Info("radical")
	})
	c.OnResponse(func(response *colly.Response) {
		log.Info(string(response.Body))
	})
	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL)
	})
	e := c.Visit(exc.URL)
	if e != nil {
		log.Error(e)
	}
	return
}

func (exc *Excavator) parseAJAX(url string, body io.Reader) (r *Radical, e error) {
	// Generated by curl-to-Go: https://mholt.github.io/curl-to-go
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	//body := strings.NewReader(`wd=%E4%B9%99`)
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header = exc.Header()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return UnmarshalRadical(bytes)
}

//ParseDocument get the url result body
func (exc *Excavator) parseDocument(url string) (doc *goquery.Document, e error) {
	var reader io.Reader
	hash := SHA256(url)
	log.Infof("hash:%s,url:%s", hash, url)
	if !exc.IsExist(hash) {
		// Request the HTML page.
		res, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
		}
		reader = res.Body
		file, e := os.OpenFile(exc.getFilePath(hash), os.O_RDWR|os.O_CREATE|os.O_SYNC, os.ModePerm)
		if e != nil {
			return nil, e

		}
		written, e := io.Copy(file, reader)
		if e != nil {
			return nil, e
		}
		log.Infof("read %s | %d ", hash, written)
		_ = file.Close()
	}
	reader, e = os.Open(exc.getFilePath(hash))
	if e != nil {
		return nil, e
	}
	// Load the HTML document
	return goquery.NewDocumentFromReader(reader)
}

// IsExist ...
func (exc *Excavator) IsExist(name string) bool {
	_, e := os.Open(name)
	return e == nil || os.IsExist(e)
}

// GetPath ...
func (exc *Excavator) getFilePath(s string) string {
	if exc.Workspace == "" {
		exc.Workspace, _ = os.Getwd()
	}
	log.With("workspace", exc.Workspace, "temp", tmpFile, "file", s).Info("file path")
	return filepath.Join(exc.Workspace, tmpFile, s)
}

/*URL 拼接地址 */
func URL(prefix string, uris ...string) string {
	end := len(prefix)
	if end > 1 && prefix[end-1] == '/' {
		prefix = prefix[:end-1]
	}

	var url = []string{prefix}
	for _, v := range uris {
		url = append(url, TrimSlash(v))
	}
	return strings.Join(url, "/")
}

// TrimSlash ...
func TrimSlash(s string) string {
	if size := len(s); size > 1 {
		if s[size-1] == '/' {
			s = s[:size-1]
		}
		if s[0] == '/' {
			s = s[1:]
		}
	}
	return s
}

func (exc *Excavator) parseCharacter(characters <-chan *RadicalCharacter, char chan<- *Character) {
	defer func() {
		char <- nil
	}()
	for {
		select {
		case cr := <-characters:
			_, e := exc.selenium.Get(URL(exc.URL, cr.URL))
			if e != nil {
				return
			}
			//TODO
		}
	}
}

func (exc *Excavator) saveRadicalCharacter(characters *RadicalCharacter) (e error) {
	i, e := exc.db.Where("url = ?", characters.URL).Count(RadicalCharacter{})
	if e != nil || i == 0 {
		return e
	}
	_, e = exc.db.InsertOne(characters)
	return
}

// SHA256 ...
func SHA256(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
}
