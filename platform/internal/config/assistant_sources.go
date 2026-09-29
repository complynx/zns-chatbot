package config

import (
	"errors"
	"strings"
)

// AssistantSources is consumed only by the Core process, never model input.
type AssistantSources struct {
	StaticQAPath  string `yaml:"static_qa_path" json:"static_qa_path"`
	AboutDocument string `yaml:"about_document" json:"about_document"`
	Credentials   Secret `yaml:"credentials"    json:"credentials"`
}

func (s AssistantSources) validate() error {
	if (s.AboutDocument == "") != (s.Credentials == "") {
		return errors.New("assistant_sources requires both about_document and credentials")
	}
	if len(s.AboutDocument) > 200 || strings.ContainsAny(s.AboutDocument, "/\\?# \t\r\n") {
		return errors.New("assistant_sources.about_document must be a document ID")
	}
	return nil
}
