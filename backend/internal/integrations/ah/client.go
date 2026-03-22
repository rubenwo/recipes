package ah

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	baseURL       = "https://api.ah.nl"
	userAgent     = "Appie/9.28 (iPhone17,3; iPhone; CPU OS 26_1 like Mac OS X)"
	clientID      = "appie-ios"
	clientVersion = "9.28"
)

// Product represents a product found in the AH product catalog.
type Product struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Price    float64 `json:"price"`
	UnitSize string  `json:"unit_size"`
	ImageURL string  `json:"image_url"`
	URL      string  `json:"url"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type searchResponse struct {
	Products []struct {
		WebshopID    int    `json:"webshopId"`
		Title        string `json:"title"`
		CurrentPrice float64 `json:"currentPrice"`
		SalesUnitSize string `json:"salesUnitSize"`
		Images       []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"products"`
}

// Client is a client for the Albert Heijn mobile API.
type Client struct {
	http        *http.Client
	mu          sync.Mutex
	token       string
	tokenExp    time.Time
	authErr     error
	authErrTime time.Time
}

// NewClient returns a new AH API client.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		http: &http.Client{Timeout: timeout},
	}
}

func (c *Client) getToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}
	// Return cached auth error for 30s to avoid hammering a broken endpoint.
	if c.authErr != nil && time.Now().Before(c.authErrTime.Add(30*time.Second)) {
		return "", c.authErr
	}

	body, _ := json.Marshal(map[string]string{"clientId": clientID})
	req, err := http.NewRequest("POST", baseURL+"/mobile-auth/v1/auth/token/anonymous", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-client-name", clientID)
	req.Header.Set("x-client-version", clientVersion)
	req.Header.Set("x-application", "AHWEBSHOP")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("AH auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		c.authErr = fmt.Errorf("AH auth failed with status %d", resp.StatusCode)
		c.authErrTime = time.Now()
		return "", c.authErr
	}
	c.authErr = nil

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("failed to decode AH token response: %w", err)
	}

	c.token = tr.AccessToken
	// Expire a minute early to avoid races
	c.tokenExp = time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return c.token, nil
}

// SearchProduct searches for a product by name and returns the best match, or
// nil if no match was found.
func (c *Client) SearchProduct(query string) (*Product, error) {
	token, err := c.getToken()
	if err != nil {
		log.Printf("AH getToken failed: %v", err)
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/mobile-services/product/search/v2?query=%s&size=5&sortOn=RELEVANCE", baseURL, url.QueryEscape(query))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-client-name", clientID)
	req.Header.Set("x-client-version", clientVersion)
	req.Header.Set("x-application", "AHWEBSHOP")

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("AH search http.Do failed for query=%q: %v", query, err)
		return nil, fmt.Errorf("AH search request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read AH search response body: %w", err)
	}
	log.Printf("AH search query=%q status=%d body=%s", query, resp.StatusCode, string(rawBody))

	// A 401 means our token expired; reset so the next call re-authenticates.
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return nil, fmt.Errorf("AH token expired, please retry")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AH search failed with status %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.Unmarshal(rawBody, &sr); err != nil {
		return nil, fmt.Errorf("failed to decode AH search response: %w", err)
	}

	if len(sr.Products) == 0 {
		return nil, nil
	}

	p := sr.Products[0]
	imageURL := ""
	if len(p.Images) > 0 {
		imageURL = p.Images[0].URL
	}
	return &Product{
		ID:       fmt.Sprintf("%d", p.WebshopID),
		Title:    p.Title,
		Price:    p.CurrentPrice,
		UnitSize: p.SalesUnitSize,
		ImageURL: imageURL,
		URL:      fmt.Sprintf("https://www.ah.nl/producten/product/wi%d", p.WebshopID),
	}, nil
}
