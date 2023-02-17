package forward

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/pkg/errors"
)

var (
	regIntegrity = regexp.MustCompile(`\sintegrity="[^"]+"`)
)

const (
	headerXProxyTarget = "X-Proxy-Target"
	headerXOriginHost  = "X-Origin-Host"
	headerXProxyClient = "X-Proxy-Client"
)

type ProxyServer struct {
	*ProxyServerOptions
	proxy *httputil.ReverseProxy
}

type ProxyServerOptions struct {
	Target               *url.URL    // proxy target
	UseSSL               bool        // use SSL
	ReqHeaders           http.Header // set request headers
	ResHeaders           http.Header // set response headers
	ProxyExternal        bool        // whether to proxy external host
	ProxyExternalIgnores []string    // the host name that should ignore when enable proxy external
	Cors                 bool        // whether enable cors
	NoCache              bool        // disabled cache for response
	OverwriteFolder      string      // overwrite request with paths
	ReplaceContent       []string    // overwrite request with paths
}

func NewProxyServer(options *ProxyServerOptions) *ProxyServer {
	proxy := httputil.NewSingleHostReverseProxy(options.Target)

	server := &ProxyServer{
		options,
		proxy,
	}

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		server.modifyRequest(req)
	}

	proxy.ModifyResponse = server.modifyResponse
	proxy.ErrorHandler = func(rw http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}
		msg := fmt.Sprintf("%+v\n", err)
		log.Println(msg)
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = rw.Write([]byte(msg))
	}

	return server
}

func (p *ProxyServer) Handler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.OverwriteFolder != "" && r.Method == http.MethodGet {
			paths := []string{p.OverwriteFolder}
			paths = append(paths, strings.Split(strings.TrimLeft(r.URL.Path, "/"), "/")...)

			proxyFilePath := filepath.Join(paths...)

			fInfo, err := os.Stat(proxyFilePath)

			// proxy request if file is not exist
			if os.IsNotExist(err) {
				p.proxy.ServeHTTP(w, r)
				return
			}

			if err != nil {
				if strings.Contains(err.Error(), "file name too long") {
					p.proxy.ServeHTTP(w, r)
					return
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(fmt.Sprintf("%+v\n", errors.WithStack(err))))
					return
				}
			}

			if fInfo.IsDir() {
				p.proxy.ServeHTTP(w, r)
				return
			}

			f, err := os.Open(proxyFilePath)

			// proxy request if file is not exist
			if os.IsNotExist(err) {
				p.proxy.ServeHTTP(w, r)
				return
			}

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(fmt.Sprintf("%+v\n", errors.WithStack(err))))
				return
			}

			defer f.Close()

			MIMEType := mime.TypeByExtension(filepath.Ext(proxyFilePath))

			w.Header().Set("Content-Type", MIMEType)
			w.WriteHeader(http.StatusOK)

			_, _ = io.Copy(w, f)
		} else {
			p.proxy.ServeHTTP(w, r)
		}
	}
}

func (p *ProxyServer) modifyRequest(req *http.Request) {
	target := *p.Target
	isProxyUrl := req.URL.Query().Get("forward_url") != ""

	if isProxyUrl {
		if unescapeUrl, err := url.QueryUnescape(strings.TrimLeft(req.URL.RawQuery, "forward_url=")); err == nil {
			if u, err := url.Parse(unescapeUrl); err == nil {
				if u.Scheme == "" {
					if p.UseSSL {
						u.Scheme = "https"
					} else {
						u.Scheme = "http"
					}
				}
				target = *u
			}
		}
	} else if req.Header.Get(headerXProxyTarget) != "" {
		targetUrl := req.Header.Get(headerXProxyTarget)

		if u, err := url.Parse(targetUrl); err == nil {
			if u.Scheme == "" {
				if p.UseSSL {
					u.Scheme = "https"
				} else {
					u.Scheme = "http"
				}
			}
			target = *u
		}
	}

	req.Header.Set(headerXOriginHost, req.Host)
	req.Host = target.Host
	if isProxyUrl {
		req.URL = &target
	} else {
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
	}

	log.Printf("[%s]: %s", req.Method, req.URL.String())

	req.Header.Set("Host", target.Host)
	req.Header.Set("Origin", fmt.Sprintf("%s://%s", target.Scheme, target.Host))
	req.Header.Set("Referrer", fmt.Sprintf("%s://%s%s", target.Scheme, target.Host, req.URL.RawPath))
	req.Header.Set("X-Real-IP", req.RemoteAddr)

	for k := range p.ReqHeaders {
		req.Header.Add(k, p.ReqHeaders.Get(k))
	}
}

func WriteFile(filename string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

func (p *ProxyServer) modifyContent(extNames []string, body []byte, originHost string, proxyHost string) []byte {
	bodyStr := string(body)

	for _, paren := range p.ReplaceContent {
		arr := strings.Split(paren, "=")

		bodyStr = strings.ReplaceAll(bodyStr, arr[0], arr[1])
	}

	bodyStr = replaceHost(bodyStr, originHost, proxyHost, p.UseSSL, p.ProxyExternal, p.ProxyExternalIgnores)

	// https://developer.mozilla.org/zh-CN/docs/Web/Security/Subresource_Integrity
	if isHtml(extNames) {
		bodyStr = regIntegrity.ReplaceAllString(bodyStr, "")

		// <meta http-equiv="Content-Security-Policy" content="default-src 'self'; img-src https://*; child-src 'none';">
		bodyStr = strings.ReplaceAll(bodyStr, `http-equiv="Content-Security-Policy"`, "") // disabled CSP
	}

	return []byte(bodyStr)
}

func (p *ProxyServer) modifyResponse(res *http.Response) error {
	target := *p.Target
	isProxyUrl := res.Request.URL.Query().Get("forward_url") != ""

	if isProxyUrl {
		if unescapeUrl, err := url.QueryUnescape(strings.TrimLeft(res.Request.URL.RawQuery, "forward_url=")); err == nil {
			if u, err := url.Parse(unescapeUrl); err == nil {
				if p.UseSSL {
					u.Scheme = "https"
				} else {
					u.Scheme = "http"
				}
				target = *u
			}
		}
	}

	proxyHost := res.Request.Header.Get(headerXOriginHost) // localhost:8080 or localhost

	var hostName string

	if strings.Contains(proxyHost, ":") {
		h, _, err := net.SplitHostPort(proxyHost)

		if err != nil {
			return errors.WithStack(err)
		}

		hostName = h
	} else {
		hostName = proxyHost
	}

	res.Header.Set(headerXProxyClient, "Forward-Cli")
	res.Header.Del("Expect-CT")

	// disable cache
	{
		if p.NoCache {
			res.Header.Set("Cache-Control", "no-cache")
		}
	}

	// disabled CSP
	{
		// https://developer.mozilla.org/zh-CN/docs/Web/HTTP/CSP
		res.Header.Del("Content-Security-Policy")
	}

	// disabled Strict-Transport-Security
	if !p.UseSSL {
		// https: //developer.mozilla.org/zh-CN/docs/Web/HTTP/Headers/Strict-Transport-Security
		res.Header.Del("Strict-Transport-Security")
	}

	// overwrite status code
	{
		// 301 -> 302
		if res.StatusCode == http.StatusMovedPermanently {
			res.StatusCode = http.StatusFound
		}
	}

	// overwrite cookies
	{
		cookies := res.Cookies()
		res.Header.Del("Set-Cookie")

		for _, v := range cookies {
			v.Domain = hostName
			if v.Secure && !p.UseSSL {
				v.Secure = false
			}

			res.Header.Add("Set-Cookie", v.String())
		}
	}

	// overrit 302 Location
	{
		// https://developer.mozilla.org/zh-CN/docs/Web/HTTP/Headers/Location
		location := res.Header.Get("Location")
		if location != "" {
			// relative path
			if !isHttpUrl(location) {
				if isProxyUrl {
					newLocation := target
					newLocation.Path = location
					res.Header.Set("Location", newLocation.String())
				}
			} else {
				newLocation := replaceHost(location, target.Host, proxyHost, p.UseSSL, p.ProxyExternal, p.ProxyExternalIgnores)
				res.Header.Set("Location", newLocation)
			}
		}
	}

	if p.Cors {
		res.Header.Set("Access-Control-Allow-Origin", "*")
		res.Header.Set("Access-Control-Allow-Credentials", "true")
	}

	for k := range p.ResHeaders {
		res.Header.Add(k, p.ResHeaders.Get(k))
	}

	// replace HTML/css/javascript... content
	{
		contentType := res.Header.Get("Content-Type")

		extNames, err := mime.ExtensionsByType(contentType)

		if err != nil {
			return nil
		}

		if !isShouldReplaceContent(extNames) {
			return nil
		}

		encoding := res.Header.Get("Content-Encoding")

		// https://developer.mozilla.org/zh-CN/docs/Web/HTTP/Headers/Content-Encoding
		switch encoding {
		case "gzip":
			reader, err := gzip.NewReader(res.Body)

			if err != nil {
				return errors.WithStack(err)
			}

			defer reader.Close()

			body, err := ioutil.ReadAll(reader)

			if err != nil {
				return errors.WithStack(err)
			}

			newBody := p.modifyContent(extNames, body, target.Host, proxyHost)

			var b bytes.Buffer
			gz := gzip.NewWriter(&b)

			if _, err := gz.Write(newBody); err != nil {
				return errors.WithStack(err)
			}

			if err := gz.Close(); err != nil {
				return errors.WithStack(err)
			}

			bin := b.Bytes()
			res.Header.Set("Content-Length", fmt.Sprint(len(bin)))
			res.Body = io.NopCloser(bytes.NewReader(bin))

		case "compress":
			// Deprecated by most browsers
		case "deflate":
			reader, err := zlib.NewReader(res.Body)

			if err != nil {
				return errors.WithStack(err)
			}

			body, err := ioutil.ReadAll(reader)

			defer reader.Close()

			if err != nil {
				return errors.WithStack(err)
			}

			newBody := p.modifyContent(extNames, body, target.Host, proxyHost)

			buf := &bytes.Buffer{}

			w := zlib.NewWriter(buf)

			if n, err := w.Write(newBody); err != nil {
				return errors.WithStack(err)
			} else if n < len(newBody) {
				return fmt.Errorf("n too small: %d vs %d for %s", n, len(newBody), string(newBody))
			}

			if err := w.Close(); err != nil {
				return errors.WithStack(err)
			}

			res.Header.Set("Content-Length", fmt.Sprint(buf.Len()))
			res.Body = io.NopCloser(buf)
		case "br":
			reader := brotli.NewReader(res.Body)

			body, err := ioutil.ReadAll(reader)

			if err != nil {
				return errors.WithStack(err)
			}

			newBody := p.modifyContent(extNames, body, target.Host, proxyHost)

			buf := &bytes.Buffer{}
			w := brotli.NewWriter(buf)
			if n, err := w.Write(newBody); err != nil {
				return errors.WithStack(err)
			} else if n < len(newBody) {
				return fmt.Errorf("n too small: %d vs %d for %s", n, len(newBody), string(newBody))
			}

			if err := w.Close(); err != nil {
				return errors.WithStack(err)
			}

			res.Header.Set("Content-Length", fmt.Sprint(buf.Len()))
			res.Body = io.NopCloser(buf)
		case "identity":
			fallthrough
		default:
			// origin response data without compress

			body, err := ioutil.ReadAll(res.Body)

			if err != nil {
				return errors.WithStack(err)
			}

			defer res.Body.Close()

			newBody := p.modifyContent(extNames, body, target.Host, proxyHost)

			if err != nil {
				return errors.WithStack(err)
			}

			res.Header.Set("Content-Length", fmt.Sprint(len(newBody)))
			res.Body = io.NopCloser(bytes.NewReader(newBody))
		}

	}

	return nil
}
