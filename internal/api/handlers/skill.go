package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tinkler/cube-agent-server/internal/api/middleware"
	"github.com/tinkler/cube-agent-server/internal/skill"
)

// SkillAdmin /admin/skill/build 端点依赖
// 实现方: *skill.Builder
type SkillAdmin interface {
	Start(intent string, auto skill.Automation) (*skill.Session, error)
	Get(sessionID string) (*skill.Session, error)
	Step2Datasource(sessionID, dsName string) (*skill.Session, error)
	Step3Analyze(sessionID string) (*skill.Session, error)
	Step4Design(sessionID string, design *skill.Design) (*skill.Session, error)
	Step5Generate(sessionID string) (*skill.Session, error)
	Step6Validate(sessionID string) (*skill.Session, error)
	Step7Publish(sessionID string) (*skill.Session, error)
	List() []*skill.Session
	Cancel(sessionID string) error
	AutoBuild(intent, dsName string) (*skill.Session, error)
}

// SkillStartRequest POST /admin/skill/build body
type SkillStartRequest struct {
	Intent     string `json:"intent"`
	Automation string `json:"automation,omitempty"`
}

// SkillStepRequest 通用 step body
type SkillStepRequest struct {
	SessionID  string `json:"session_id"`
	Datasource string `json:"datasource,omitempty"`
}

// SkillStep4Request step 4 design body
type SkillStep4Request struct {
	SessionID       string   `json:"session_id"`
	CubeName        string   `json:"cube_name"`
	CubeDescription string   `json:"cube_description"`
	SQLTemplate     string   `json:"sql_template"`
	PrimaryKey      string   `json:"primary_key"`
	Measures        []string `json:"measures"`
	Dimensions      []string `json:"dimensions"`
	Segments        []string `json:"segments"`
}

// SkillStart POST /admin/skill/build
func SkillStart(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStartRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		if body.Intent == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "intent required",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		auto := skill.Automation(body.Automation)
		if auto == "" {
			auto = skill.SemiAuto
		}
		s, err := admin.Start(body.Intent, auto)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"session":    s,
			"next_step":  "step 2: choose datasource",
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// SkillGet GET /admin/skill/session/:id
func SkillGet(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		s, err := admin.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":      err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"session":    s,
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// SkillList GET /admin/skill/sessions
func SkillList(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"sessions":   admin.List(),
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// SkillStep2 POST /admin/skill/step/datasource
func SkillStep2(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStepRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		s, err := admin.Step2Datasource(body.SessionID, body.Datasource)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"session": s, "request_id": middleware.GetRequestID(c)})
	}
}

// SkillStep3 POST /admin/skill/step/analyze
func SkillStep3(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStepRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		s, err := admin.Step3Analyze(body.SessionID)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"session": s, "request_id": middleware.GetRequestID(c)})
	}
}

// SkillStep4 POST /admin/skill/step/design
func SkillStep4(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStep4Request
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		design := &skill.Design{
			CubeName:        body.CubeName,
			CubeDescription: body.CubeDescription,
			SQLTemplate:     body.SQLTemplate,
			PrimaryKey:      body.PrimaryKey,
			Measures:        body.Measures,
			Dimensions:      body.Dimensions,
			Segments:        body.Segments,
		}
		s, err := admin.Step4Design(body.SessionID, design)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"session": s, "request_id": middleware.GetRequestID(c)})
	}
}

// SkillStep5 POST /admin/skill/step/generate
func SkillStep5(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStepRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		s, err := admin.Step5Generate(body.SessionID)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		yaml := ""
		if s.Draft != nil {
			yaml = s.Draft.YAML
		}
		c.JSON(http.StatusOK, gin.H{
			"session":      s,
			"yaml_preview": yaml,
			"request_id":   middleware.GetRequestID(c),
		})
	}
}

// SkillStep6 POST /admin/skill/step/validate
func SkillStep6(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStepRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		s, err := admin.Step6Validate(body.SessionID)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"session": s, "request_id": middleware.GetRequestID(c)})
	}
}

// SkillStep7 POST /admin/skill/step/publish
func SkillStep7(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillStepRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		s, err := admin.Step7Publish(body.SessionID)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"session":    s,
			"path":       s.PublishedPath,
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// SkillCancel DELETE /admin/skill/session/:id
func SkillCancel(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := admin.Cancel(id); err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "request_id": middleware.GetRequestID(c)})
	}
}

// SkillAutoBuildRequest 自动 build body
type SkillAutoBuildRequest struct {
	Intent     string `json:"intent"`
	Datasource string `json:"datasource"`
}

// SkillAutoBuild POST /admin/skill/auto-build
// 一气呵成跑完 7 步,无需用户交互
func SkillAutoBuild(admin SkillAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body SkillAutoBuildRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondBadRequest(c, err.Error())
			return
		}
		if body.Intent == "" || body.Datasource == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "intent and datasource required",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		s, err := admin.AutoBuild(body.Intent, body.Datasource)
		if err != nil {
			respondInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"session":     s,
			"path":        s.PublishedPath,
			"cube_name":   s.Design.CubeName,
			"request_id":  middleware.GetRequestID(c),
		})
	}
}

// ============================================================
// helpers
// ============================================================

func respondBadRequest(c *gin.Context, detail string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error":      "invalid body",
		"detail":     detail,
		"request_id": middleware.GetRequestID(c),
	})
}

func respondInternal(c *gin.Context, detail string) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error":      "internal error",
		"detail":     detail,
		"request_id": middleware.GetRequestID(c),
	})
}
