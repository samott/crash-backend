package rates

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"math"

	"github.com/shopspring/decimal"
);

const FREE_API_URL = "https://api.coingecko.com/api";
const PRO_API_URL = "https://pro-api.coingecko.com/api";

/**
 * ApiKey is the CoinGecko API key; use the empty string for basic API
 * access (with lower access limits_
 *
 * Cryptos is a map of localId -> remoteId pairs.
 *
 * Fiats is an array of fiat currencies for which we want rates.
 */
type RatesConfig struct {
	ApiKey string;
	Cryptos map[string]string;
	Fiats []string;
}

type Rates struct {
	isPro bool;
	config RatesConfig;
}

type FiatRates = map[string]decimal.Decimal;
type RatesResult = map[string]FiatRates;

func NewService(config *RatesConfig) (*Rates) {
	return &Rates{
		isPro: config.ApiKey != "",
		config: *config,
	};
}

func (rates *Rates) FetchRates() (RatesResult, error) {
	localIds := make([]string, 0, len(rates.config.Cryptos))
	remoteIds := make([]string, 0, len(rates.config.Cryptos))
	remote2Local := make(map[string]string);

	result := make(RatesResult);

	req, _ := http.NewRequest("GET", rates.getUrl("/v3/simple/price"), nil);

	for localId, remoteId := range rates.config.Cryptos {
		localIds = append(localIds, localId);
		remoteIds = append(remoteIds, remoteId);
		remote2Local[remoteId] = localId;
	}

	query := req.URL.Query();
	query.Add("ids", strings.Join(remoteIds, ","));
	query.Add("vs_currencies", strings.Join(rates.config.Fiats, ","));
	req.URL.RawQuery = query.Encode();

	client := &http.Client{};

	if rates.isPro {
		req.Header.Set("X-CG-Pro-API-Key", rates.config.ApiKey);
	}

	res, err := client.Do(req);

	if err != nil {
		return result, err;
	}

	defer res.Body.Close();

	if res.StatusCode > 299 || res.StatusCode < 200 {
		return result, errors.New("API returned " + res.Status);
	}

	decoder := json.NewDecoder(res.Body);

	var priceMap map[string]map[string]float64;

	decoder.Decode(&priceMap);

	if len(priceMap) != len(remoteIds) {
		return result, errors.New("Failed to fetch all currencies");
	}

	for remoteId := range(priceMap) {
		localId := remote2Local[remoteId];

		result[localId] = make(FiatRates);

		for fiatId, rate := range(priceMap[remoteId]) {
			if (math.IsNaN(rate) || math.IsInf(rate, 0)) {
				return result, nil;
			}

			result[localId][fiatId] = decimal.NewFromFloat(rate);
		}
	}

	return result, nil;
}

func (rates *Rates) getUrl(path string) (string) {
	if rates.isPro {
		return PRO_API_URL + path;
	} else {
		return FREE_API_URL + path;
	}
}
