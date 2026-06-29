package main

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail"`
}

type request struct {
	Method string
	Path   string
	Token  string
	Body   any
}

type apiClient struct {
	baseURL string
	http    *http.Client
}

type authDetail struct {
	User  userDetail `json:"user"`
	Token string     `json:"token"`
}

type userDetail struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type resourceID struct {
	ID string `json:"id"`
}

type branch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mcpToken struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type seedResult struct {
	AdminEmail   string
	WriterEmail  string
	TeamID       string
	ProjectID    string
	DocumentID   string
	VersionOneID string
	VersionTwoID string
	DiffID       string
	MCPTokenID   string
}
