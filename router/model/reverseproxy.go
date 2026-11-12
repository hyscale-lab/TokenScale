package model

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

// wrapper class, easy for mocking/testing in the function
type ReverseProxy interface {
	ServeHTTP(c context.Context, ctx *app.RequestContext)
}