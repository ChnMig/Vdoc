package shared

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func PageQuery(c *gin.Context) (app.PageQuery, error) {
	query := app.PageQuery{Limit: 50, Search: strings.TrimSpace(c.Query("search"))}
	for _, field := range []struct {
		name   string
		target *int
	}{{"page_size", &query.Limit}, {"offset", &query.Offset}} {
		if raw, exists := c.GetQuery(field.name); exists {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return query, fmt.Errorf("%w: invalid %s", app.ErrInvalidArgument, field.name)
			}
			*field.target = value
		}
	}
	return query, query.Validate()
}

func QueryTime(c *gin.Context, name string) (*time.Time, error) {
	raw := c.Query(name)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must use RFC3339", app.ErrInvalidArgument, name)
	}
	return &value, nil
}
