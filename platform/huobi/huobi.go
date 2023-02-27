package huobi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"scanner_bot/config"
	"scanner_bot/platform"
	"strconv"
	"strings"
	"sync"
)

type Platform struct {
	*platform.PlatformTemplate
}

func New(name string, url string, tradeTypes []string, tokens []string, tokensDict map[string]string, payTypesDict map[string]string, allPairs map[string]bool) *Platform {
	return &Platform{
		PlatformTemplate: platform.New(name, url, tradeTypes, tokens, tokensDict, payTypesDict, allPairs),
	}
}

func (p *Platform) GetResult(c *config.Configuration) (*platform.ResultPlatformData, error) {
	result := platform.ResultPlatformData{}
	wg := sync.WaitGroup{}
	result.Name = p.Name

	wg.Add(1)
	go func() {
		spotData, err := p.getSpotData()
		if err != nil {
			log.Printf("can't get spot data: %v", err)
		}
		result.Spot = *spotData
		defer wg.Done()
	}()


	result.Tokens = map[string]*platform.TokenInfo{}

	for _, token := range p.Tokens {
		token:=token
		cryptoToken := p.TokensDict[token]
		tokenInfo := &platform.TokenInfo{}
		result.Tokens[cryptoToken] = tokenInfo

		wg.Add(1)
		go func() {
			buy, err := p.getAdvertise(c, token, p.TradeTypes[0])
			if err != nil || buy == nil {
				log.Printf("can't get buy advertise for huobi, token (%s): %v", token, err)
			} else {
				tokenInfo.Buy = *buy
			}
			defer wg.Done()
		}()


		wg.Add(1)
		go func() {
			sell, err := p.getAdvertise(c, token, p.TradeTypes[1])
			if err != nil || sell == nil {
				log.Printf("can't get sell advertise for huobi, token (%s): %v", token, err)
			} else {
				tokenInfo.Sell = *sell
			}
			defer wg.Done()
		}()
		//result.Tokens[token] = tokenInfo

	}
	wg.Wait()

	return &result, nil
}
func (p *Platform) getSpotData() (*map[string]float64, error) {
	//create Req
	url := "https://api.huobi.pro/market/tickers"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("can't get request to spot (huobi): %w", err)
	}
	//Do req (need to fix and create common DoGetRequestFunc)
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't get resposnse from spot (huobi): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("can't read info from response: %w", err)
	}
	var spotResponse SpotResponse
	if err := json.Unmarshal(body, &spotResponse); err != nil {
		return nil, fmt.Errorf("can't unmarshall: %w", err)
	}

	result := map[string]float64{}
	set := p.AllPairs

	for _, item := range spotResponse.Data {
		_, ok := set[strings.ToUpper(item.Symbol)]
		if ok {
			result[strings.ToUpper(item.Symbol)] = item.Close
		}
	}
	return &result, err

}

func (p *Platform) getAdvertise(c *config.Configuration, token string, tradeType string) (*platform.Advertise, error) {
	userConfig := &c.UserConfig
	query := p.getQuery(userConfig, token, tradeType)
	response, err := p.doRequest(query)
	if err != nil {
		return nil, fmt.Errorf("can't do request to get bybit response: %w", err)
	}

	advertise, err := p.responseToAdvertise(response)
	if err != nil {
		return nil, fmt.Errorf("can't convert response to Advertise: %w", err)
	}

	return advertise, nil
}

func (p *Platform) getQuery(c *config.Config, token string, tradeType string) string {
	u := url.Values{
		"coinId":       []string{token},     //usdt
		"currency":     []string{"11"},      //rub
		"tradeType":    []string{tradeType}, //buy
		"currPage":     []string{"1"},
		"payMethod":    []string{strings.Join(p.GetPayTypes(c), ",")},
		"acceptOrder":  []string{"0"},
		"country":      []string{""},
		"blockType":    []string{"general"},
		"online":       []string{"1"},
		"range":        []string{"0"},
		"amount":       []string{strconv.Itoa(c.MinValue)}, //amount
		"isThumbsUp":   []string{"false"},
		"isMerchant":   []string{"false"},
		"isTraded":     []string{"false"},
		"onlyTradable": []string{"false"},
		"isFollowed":   []string{"false"},
	}

	return u.Encode()
}

func (p *Platform) doRequest(query string) (*[]byte, error) {
	req, err := http.NewRequest(http.MethodGet, p.Url, nil)
	req.URL.RawQuery = query
	if err != nil {
		return nil, fmt.Errorf("can't do huobi request: %w", err)
	}
	req.Header.Set("accept", "*/*")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", `Mozilla/5.0 (Linux; Android 6.0; Nexus 5 Build/MRA58N) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/110.0.0.0 Mobile Safari/537.36`)

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't get response: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("can't read body: %w", err)
	}
	return &body, nil
}

func (p *Platform) responseToAdvertise(response *[]byte) (*platform.Advertise, error) {
	var data Response
	err := json.Unmarshal(*response, &data)
	if err != nil || len(data.Data) == 0 || data.Code != 200 {
		return nil, fmt.Errorf("cant' unmarshall data from huobi response: %w", err)
	}
	item := data.Data[0]

	cost, _ := strconv.ParseFloat(item.Price, 64)
	minLimit, _ := strconv.ParseFloat(item.MinTradeLimit, 64)
	maxLimit, _ := strconv.ParseFloat(item.MaxTradeLimit, 64)
	available, _ := strconv.ParseFloat(item.TradeCount, 64)
	pays := getStringSlice(item.PayMethods)

	return &platform.Advertise{
		PlatformName: p.Name,
		SellerName:   item.UserName,
		Asset:        p.TokenFromDict(strconv.Itoa(item.CoinID)),
		Fiat:         strconv.Itoa(item.Currency) + " (RUB)",
		BankName:     p.PayTypesToString(pays),
		Cost:         cost,
		MinLimit:     minLimit,
		MaxLimit:     maxLimit,
		SellerDeals:  item.TradeMonthTimes,
		TradeType:    huobiTradeType(item.TradeType),
		Available:    available,
	}, nil
}
