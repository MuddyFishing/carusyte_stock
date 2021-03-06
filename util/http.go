package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/carusyte/stock/conf"
	"github.com/pkg/errors"
	logr "github.com/sirupsen/logrus"
	"golang.org/x/net/proxy"
)

const RETRY int = 3

var PART_PROXY float64
var PROXY_ADDR string

//HTTPGetResponse initiates HTTP get request and returns its response
func HTTPGetResponse(link string, headers map[string]string, useMasterProxy, rotateProxy, rotateAgent bool) (res *http.Response, e error) {
	if useMasterProxy && rotateProxy {
		log.Panic("can't useMasterProxy and rotateProxy at the same time.")
	}

	host := ""
	r := regexp.MustCompile(`//([^/]*)/`).FindStringSubmatch(link)
	if len(r) > 0 {
		host = r[len(r)-1]
	}

	var client *http.Client
	//determine if we must use a master proxy
	bypassed := false
	if rotateProxy && rand.Float32() <= conf.Args.Network.RotateProxyBypassRatio {
		bypassed = true
	}
	if useMasterProxy {
		// create a socks5 dialer
		dialer, err := proxy.SOCKS5("tcp", conf.Args.Network.MasterProxyAddr, nil, proxy.Direct)
		if err != nil {
			log.Printf("can't connect to the master proxy: %+v", err)
			return nil, errors.WithStack(err)
		}
		// setup a http client
		httpTransport := &http.Transport{Dial: dialer.Dial}
		client = &http.Client{Timeout: time.Second * 60, // Maximum of 60 secs
			Transport: httpTransport}
	} else if !rotateProxy || bypassed {
		client = &http.Client{Timeout: time.Second * 60} // Maximum of 60 secs
	}

	for i := 0; true; i++ {
		req, err := http.NewRequest(http.MethodGet, link, nil)
		if err != nil {
			log.Panic(err)
		}

		req.Header.Set("Accept", "text/html,application/xhtml+xml,"+
			"application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7,zh-TW;q=0.6")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Connection", "close")
		if host != "" {
			req.Header.Set("Host", host)
		}
		req.Header.Set("Pragma", "no-cache")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		uagent := ""
		if rotateAgent {
			uagent, e = PickUserAgent()
			if e != nil {
				log.Printf("failed to acquire rotate user agent: %+v", e)
				time.Sleep(time.Millisecond * time.Duration(300+rand.Intn(300)))
				continue
			}
		} else {
			uagent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_1) " +
				"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/62.0.3202.94 Safari/537.36"
		}
		req.Header.Set("User-Agent", uagent)
		if headers != nil && len(headers) > 0 {
			for k, hv := range headers {
				req.Header.Set(k, hv)
			}
		}

		if rotateProxy && !bypassed {
			//determine if we must use a rotated proxy
			proxyAddr, e := PickProxy()
			if strings.HasPrefix(proxyAddr, "socks5://") {
				// create a socks5 dialer
				dialer, err := proxy.SOCKS5("tcp", strings.TrimLeft(proxyAddr, "socks5://"), nil, proxy.Direct)
				if err != nil {
					log.Printf("can't connect to the socks5 proxy: %+v", err)
					return nil, errors.WithStack(err)
				}
				// setup a http client
				httpTransport := &http.Transport{Dial: dialer.Dial}
				client = &http.Client{Timeout: time.Second * 60, // Maximum of 60 secs
					Transport: httpTransport}
			} else {
				//http proxy
				if e != nil {
					log.Printf("failed to acquire rotate proxy: %+v", e)
					return nil, errors.WithStack(e)
				}
				proxyURL, e := url.Parse(proxyAddr)
				if e != nil {
					log.Printf("invalid proxy: %s, %+v", proxyAddr, e)
					return nil, errors.WithStack(e)
				}
				// setup a http client
				client = &http.Client{
					Timeout:   time.Second * 60, // Maximum of 60 secs
					Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
			}
		}

		res, err = client.Do(req)
		if err != nil {
			//handle "read: connection reset by peer" error by retrying
			if i >= RETRY {
				log.Printf("http communication failed. url=%s\n%+v", link, err)
				e = err
				return
			}
			log.Printf("http communication error. url=%s, retrying %d ...\n%+v", link, i+1, err)
			if res != nil {
				res.Body.Close()
			}
			time.Sleep(time.Millisecond * time.Duration(500+rand.Intn(300)))
		} else {
			return
		}
	}
	return
}

func HttpGetResp(link string) (res *http.Response, e error) {
	return HttpGetRespUsingHeaders(link, nil)
}

func HttpGetRespUsingHeaders(link string, headers map[string]string) (res *http.Response, e error) {
	host := ""
	r := regexp.MustCompile(`//([^/]*)/`).FindStringSubmatch(link)
	if len(r) > 0 {
		host = r[len(r)-1]
	}

	var client *http.Client
	//determine if we must use a proxy
	if PART_PROXY > 0 && rand.Float64() < PART_PROXY {
		// create a socks5 dialer
		dialer, err := proxy.SOCKS5("tcp", PROXY_ADDR, nil, proxy.Direct)
		if err != nil {
			fmt.Fprintln(os.Stderr, "can't connect to the proxy:", err)
			os.Exit(1)
		}
		// setup a http client
		httpTransport := &http.Transport{Dial: dialer.Dial}
		client = &http.Client{Timeout: time.Second * 60, // Maximum of 60 secs
			Transport: httpTransport}
	} else {
		client = &http.Client{Timeout: time.Second * 60} // Maximum of 60 secs

	}

	for i := 0; true; i++ {
		req, err := http.NewRequest(http.MethodGet, link, nil)
		if err != nil {
			log.Panic(err)
		}

		req.Header.Set("Accept", "text/html,application/xhtml+xml,"+
			"application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7,zh-TW;q=0.6")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Connection", "close")
		//req.Header.Set("Cookie", "searchGuide=sg; "+
		//	"UM_distinctid=15d4e2ca50580-064a0c1f749ffa-30667808-232800-15d4e2ca506a9c; "+
		//	"Hm_lvt_78c58f01938e4d85eaf619eae71b4ed1=1502162404,1502164752,1504536800; "+
		//	"Hm_lpvt_78c58f01938e4d85eaf619eae71b4ed1=1504536800")
		if host != "" {
			req.Header.Set("Host", host)
		}
		req.Header.Set("Pragma", "no-cache")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("User-Agent",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_1) "+
				"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/62.0.3202.94 Safari/537.36")
		if headers != nil && len(headers) > 0 {
			for k, hv := range headers {
				req.Header.Set(k, hv)
			}
		}

		res, err = client.Do(req)
		if err != nil {
			//handle "read: connection reset by peer" error by retrying
			if i >= RETRY {
				log.Printf("http communication failed. url=%s\n%+v", link, err)
				e = err
				return
			} else {
				log.Printf("http communication error. url=%s, retrying %d ...\n%+v", link, i+1, err)
				if res != nil {
					res.Body.Close()
				}
				time.Sleep(time.Millisecond * 500)
			}
		} else {
			return
		}
	}
	return
}

func HttpGetBytesUsingHeaders(link string, headers map[string]string) (body []byte, e error) {
	var resBody *io.ReadCloser
	defer func() {
		if resBody != nil {
			(*resBody).Close()
		}
	}()
	for i := 0; true; i++ {
		res, e := HttpGetRespUsingHeaders(link, headers)
		if e != nil {
			if i >= RETRY {
				log.Printf("http communication failed. url=%s\n%+v", link, e)
				return nil, e
			}
			log.Printf("http communication error. url=%s, retrying %d ...\n%+v", link, i+1, e)
			if res != nil {
				res.Body.Close()
			}
			time.Sleep(time.Millisecond * 500)
			continue
		}
		resBody = &res.Body
		var err error
		body, err = ioutil.ReadAll(res.Body)
		if err != nil {
			//handle "read: connection reset by peer" error by retrying
			if i >= RETRY {
				log.Printf("http communication failed. url=%s\n%+v", link, err)
				return nil, e
			}
			log.Printf("http communication error. url=%s, retrying %d ...\n%+v", link, i+1, err)
			if resBody != nil {
				res.Body.Close()
			}
			time.Sleep(time.Millisecond * 500)
			continue
		} else {
			return body, nil
		}
	}
	return
}

//HTTPPostJSON visits url using http post method, optionally using provided headers.
// params will be marshalled to json format before sending to the url server.
func HTTPPostJSON(link string, headers, params map[string]string) (body []byte, e error) {
	var resBody *io.ReadCloser
	defer func() {
		if resBody != nil {
			(*resBody).Close()
		}
	}()

	var client *http.Client
	//determine if we must use a proxy
	if PART_PROXY > 0 && rand.Float64() < PART_PROXY {
		// create a socks5 dialer
		dialer, err := proxy.SOCKS5("tcp", PROXY_ADDR, nil, proxy.Direct)
		if err != nil {
			fmt.Fprintln(os.Stderr, "can't connect to the proxy:", err)
			os.Exit(1)
		}
		// setup a http client
		httpTransport := &http.Transport{}
		client = &http.Client{Timeout: time.Second * 60, // Maximum of 60 secs
			Transport: httpTransport}
		// set our socks5 as the dialer
		httpTransport.Dial = dialer.Dial
	} else {
		client = &http.Client{Timeout: time.Second * 60} // Maximum of 60 secs
	}

	for i := 0; true; i++ {
		jsonParams, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		logr.Debugf("HTTP Post param: %+v", params)
		req, err := http.NewRequest(
			http.MethodPost,
			link,
			bytes.NewBuffer((jsonParams)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if headers != nil && len(headers) > 0 {
			for k, hv := range headers {
				req.Header.Set(k, hv)
			}
		}
		res, err := client.Do(req)
		if err != nil {
			//handle "read: connection reset by peer" error by retrying
			if i >= RETRY {
				log.Printf("http communication failed. url=%s\n%+v", link, err)
				e = err
				return
			}
			log.Printf("http communication error. url=%s, retrying %d ...\n%+v", link, i+1, err)
			if res != nil {
				res.Body.Close()
			}
			time.Sleep(time.Millisecond * 500)
		} else {
			resBody = &res.Body
			body, err = ioutil.ReadAll(res.Body)
			if err != nil {
				//handle "read: connection reset by peer" error by retrying
				if i >= RETRY {
					log.Printf("http communication failed. url=%s\n%+v", link, err)
					return nil, e
				}
				log.Printf("http communication error. url=%s, retrying %d ...\n%+v", link, i+1, err)
				if resBody != nil {
					res.Body.Close()
				}
				time.Sleep(time.Millisecond * 500)
				continue
			} else {
				return body, nil
			}
		}
	}
	return
}

func HttpGetBytes(link string) (body []byte, e error) {
	return HttpGetBytesUsingHeaders(link, nil)
}

func Download(link, file string) (err error) {
	return DownloadUsingHeaders(link, file, nil)
}

func DownloadUsingHeaders(link, file string, headers map[string]string) (e error) {
	log.Printf("Downloading from %s", link)

	if _, e := os.Stat(file); e == nil {
		os.Remove(file)
	}

	output, err := os.Create(file)
	if err != nil {
		log.Printf("Error while creating %s\n%+v", file, err)
		return
	}
	defer output.Close()

	response, err := HttpGetRespUsingHeaders(link, headers)
	if err != nil {
		log.Printf("Error while downloading %s\n%+v", link, err)
		return
	}
	defer response.Body.Close()

	n, err := io.Copy(output, response.Body)
	if err != nil {
		log.Printf("Error while downloading %s\n%+v", link, err)
		return
	}

	log.Printf("%d bytes downloaded. file saved to %s.", n, file)

	return nil
}
