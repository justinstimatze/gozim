package zim

import (
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/analysis/lang/fr"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/registry"
)

func analyzerConstructorEn(config map[string]interface{}, cache *registry.Cache) (analysis.Analyzer, error) {
	tokenizer, err := cache.TokenizerNamed(unicode.Name)
	if err != nil {
		return nil, err
	}
	possEnFilter, err := cache.TokenFilterNamed(en.PossessiveName)
	if err != nil {
		return nil, err
	}
	toLowerFilter, err := cache.TokenFilterNamed(lowercase.Name)
	if err != nil {
		return nil, err
	}
	stopEnFilter, err := cache.TokenFilterNamed(en.StopName)
	if err != nil {
		return nil, err
	}

	rv := analysis.DefaultAnalyzer{
		Tokenizer: tokenizer,
		TokenFilters: []analysis.TokenFilter{
			possEnFilter,
			toLowerFilter,
			stopEnFilter,
		},
	}
	return &rv, nil
}

func analyzerConstructorFr(config map[string]interface{}, cache *registry.Cache) (analysis.Analyzer, error) {
	tokenizer, err := cache.TokenizerNamed(unicode.Name)
	if err != nil {
		return nil, err
	}
	toLowerFilter, err := cache.TokenFilterNamed(lowercase.Name)
	if err != nil {
		return nil, err
	}
	stopFrFilter, err := cache.TokenFilterNamed(fr.StopName)
	if err != nil {
		return nil, err
	}
	elisionFrFilter, err := cache.TokenFilterNamed(fr.ElisionName)
	if err != nil {
		return nil, err
	}

	rv := analysis.DefaultAnalyzer{
		Tokenizer: tokenizer,
		TokenFilters: []analysis.TokenFilter{
			toLowerFilter,
			elisionFrFilter,
			stopFrFilter,
		},
	}
	return &rv, nil
}

func init() {
	registry.RegisterAnalyzer("ennostemm", analyzerConstructorEn)
	registry.RegisterAnalyzer("frnostemm", analyzerConstructorFr)
}
