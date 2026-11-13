package cloudconnect

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// S3Connector implements Connector for Amazon S3 (read-only) using AWS SigV4.
type S3Connector struct {
	id       string
	bucket   string
	region   string
	endpoint string
	sec      SecretBundle

	mu sync.Mutex
}

func NewS3Connector(id, rootPath string, sec SecretBundle) (*S3Connector, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("bucket (root_path) required for s3")
	}
	region := sec.Region
	if region == "" {
		region = "us-east-1"
	}
	endpoint := sec.Endpoint
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	endpoint = strings.TrimRight(endpoint, "/")
	return &S3Connector{id: id, bucket: rootPath, region: region, endpoint: endpoint, sec: sec}, nil
}

func (c *S3Connector) Provider() string { return "s3" }
func (c *S3Connector) ID() string       { return c.id }

func (c *S3Connector) host() string {
	// virtual-hosted style: bucket.s3.region.amazonaws.com
	if strings.Contains(c.endpoint, "amazonaws.com") {
		return c.bucket + ".s3." + c.region + ".amazonaws.com"
	}
	return "" // path style handled in url
}

func (c *S3Connector) baseURL() string {
	if strings.Contains(c.endpoint, "amazonaws.com") {
		return "https://" + c.bucket + ".s3." + c.region + ".amazonaws.com"
	}
	return c.endpoint + "/" + c.bucket
}

func (c *S3Connector) sign(req *http.Request, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	// Canonical headers
	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeadersStr := strings.Join(signedHeaders, ";")

	// Canonical query string (sorted)
	q := req.URL.Query()
	var keys []string
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var qparts []string
	for _, k := range keys {
		for _, v := range q[k] {
			qparts = append(qparts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	canonicalQuery := strings.Join(qparts, "&")

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalRequest := req.Method + "\n" +
		canonicalURI + "\n" +
		canonicalQuery + "\n" +
		canonicalHeaders + "\n" +
		signedHeadersStr + "\n" +
		payloadHash

	// String to sign
	credentialScope := dateStamp + "/" + c.region + "/s3/aws4_request"
	h := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + hex.EncodeToString(h[:])

	// Signing key
	kDate := hmacSHA256([]byte("AWS4"+c.sec.SecretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(c.region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	auth := "AWS4-HMAC-SHA256 Credential=" + c.sec.AccessKey + "/" + credentialScope +
		", SignedHeaders=" + signedHeadersStr + ", Signature=" + signature
	req.Header.Set("Authorization", auth)
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

type s3ListResult struct {
	XMLName  xml.Name `xml:"ListBucketResult"`
	Contents []struct {
		Key          string `xml:"Key"`
		Size         int64  `xml:"Size"`
		LastModified string `xml:"LastModified"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
	IsTruncated bool   `xml:"IsTruncated"`
	NextToken   string `xml:"NextContinuationToken"`
}

func (c *S3Connector) List(ctx context.Context, path string) ([]Entry, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	prefix := path
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	base := c.baseURL()
	u := base + "/?list-type=2&max-keys=200"
	if prefix != "" {
		u += "&prefix=" + url.QueryEscape(prefix)
	}
	u += "&delimiter=%2F"

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, sha256Hex(""))
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := readAllBody(resp)
		return nil, fmt.Errorf("s3 list failed: %s %s", resp.Status, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var res s3ListResult
	if err := xml.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	entries := []Entry{}
	seen := map[string]bool{}
	for _, cp := range res.CommonPrefixes {
		name := strings.TrimSuffix(strings.TrimPrefix(cp.Prefix, prefix), "/")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, Entry{
			ID:       cp.Prefix,
			Name:     name,
			Path:     strings.TrimSuffix(cp.Prefix, "/"),
			IsDir:    true,
			MimeType: "application/x-directory",
		})
	}
	for _, c2 := range res.Contents {
		if c2.Key == prefix {
			continue
		}
		name := strings.TrimPrefix(c2.Key, prefix)
		if strings.Contains(name, "/") {
			continue
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		mod, _ := time.Parse(time.RFC3339, c2.LastModified)
		entries = append(entries, Entry{
			ID:       c2.Key,
			Name:     name,
			Path:     c2.Key,
			IsDir:    false,
			Size:     c2.Size,
			Modified: mod,
		})
	}
	return entries, nil
}

func (c *S3Connector) Read(ctx context.Context, path string) (io.ReadCloser, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL()+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, sha256Hex(""))
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := readAllBody(resp)
		return nil, fmt.Errorf("s3 read failed: %s %s", resp.Status, string(b))
	}
	return resp.Body, nil
}

func (c *S3Connector) Metadata(ctx context.Context, path string) (*Entry, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	req, err := http.NewRequestWithContext(ctx, "HEAD", c.baseURL()+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, sha256Hex(""))
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("s3 metadata failed: %s", resp.Status)
	}
	size := int64(0)
	fmt.Sscan(resp.Header.Get("Content-Length"), &size)
	mod, _ := time.Parse(time.RFC1123, resp.Header.Get("Last-Modified"))
	name := path
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return &Entry{
		ID:       path,
		Name:     name,
		Path:     path,
		IsDir:    false,
		Size:     size,
		Modified: mod,
	}, nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (c *S3Connector) Write(ctx context.Context, path string, r io.Reader, size int64) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path")
	}
	path = ResolveConflictPath(c, path)

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	h := sha256.Sum256(data)
	payloadHash := hex.EncodeToString(h[:])

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL()+"/"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.sign(req, payloadHash)
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = int64(len(data))
	}
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3 write failed: %s %s", resp.Status, string(b))
	}
	return nil
}

func (c *S3Connector) Delete(ctx context.Context, path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL()+"/"+path, nil)
	if err != nil {
		return err
	}
	c.sign(req, sha256Hex(""))
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3 delete failed: %s %s", resp.Status, string(b))
	}
	return nil
}
