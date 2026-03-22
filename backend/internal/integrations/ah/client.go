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
	baseURL   = "https://api.ah.nl"
	userAgent = "Appie/8.22.3"
	clientID  = "appie"
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
	Cards []struct {
		Type     string `json:"type"`
		Products []struct {
			WebshopID string `json:"webshopId"`
			Title     string `json:"title"`
			Price     struct {
				Now      float64 `json:"now"`
				UnitSize string  `json:"unitSize"`
			} `json:"price"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
			Link string `json:"link"`
		} `json:"products"`
	} `json:"cards"`
}

// Client is a client for the Albert Heijn mobile API.
type Client struct {
	http     *http.Client
	mu       sync.Mutex
	token    string
	tokenExp time.Time
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

	body, _ := json.Marshal(map[string]string{"clientId": clientID})
	req, err := http.NewRequest("POST", baseURL+"/mobile/v4/session", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("AH auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("AH auth failed with status %d", resp.StatusCode)
	}

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
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/mobile/v2/product/search?query=%s&size=5", baseURL, url.QueryEscape(query))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AH search request failed: %w", err)
	}
	defer resp.Body.Close()

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

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read AH search response body: %w", err)
	}
	log.Printf("AH search query=%q status=%d body=%s", query, resp.StatusCode, string(rawBody))

	var sr searchResponse
	if err := json.Unmarshal(rawBody, &sr); err != nil {
		return nil, fmt.Errorf("failed to decode AH search response: %w", err)
	}

	for _, card := range sr.Cards {
		log.Printf("AH card type=%q products=%d", card.Type, len(card.Products))
		if card.Type == "default" && len(card.Products) > 0 {
			p := card.Products[0]
			imageURL := ""
			if len(p.Images) > 0 {
				imageURL = p.Images[0].URL
			}
			return &Product{
				ID:       p.WebshopID,
				Title:    p.Title,
				Price:    p.Price.Now,
				UnitSize: p.Price.UnitSize,
				ImageURL: imageURL,
				URL:      "https://www.ah.nl" + p.Link,
			}, nil
		}
	}

	// No matching product found
	return nil, nil
}
