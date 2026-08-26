package main

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

func sanitizedRecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() == nil {
				return
			}
			method := ""
			path := ""
			if c.Request != nil {
				method = c.Request.Method
				if c.Request.URL != nil {
					path = c.Request.URL.Path
				}
			}
			log.Printf("panic recovered: method=%s path=%s\n%s", method, path, debug.Stack())
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}()
		c.Next()
	}
}
