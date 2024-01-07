package controllers

import (
	"encoding/json"
	"github.com/beego/beego/utils/pagination"
	"github.com/casbin/casvisor/object"
	"github.com/casbin/casvisor/util"
)

// GetSessions
// @Title GetSessions
// @Tag Session API
// @Description get all sessions
// @Param   pageSize     query    string  true        "The size of each page"
// @Param   p     query    string  true        "The number of the page"
// @Success 200 {object} object.Session The Response object
// @router /get-sessions [get]
func (c *ApiController) GetSessions() {
	_, ok := c.RequireAdmin()
	if !ok {
		//
		return
	}
	owner := c.Input().Get("owner")
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		sessions, err := object.GetSessions(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(sessions)
	} else {
		limit := util.ParseInt(limit)

		count, err := object.GetSessionCount(owner, field, value)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		paginator := pagination.SetPaginator(c.Ctx, limit, count)
		sessions, err := object.GetPaginationSessions(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(sessions, paginator.Nums())
	}
}

// GetConnSession
// @Title GetConnSession
// @Tag Session API
// @Description get session
// @Param   id     query    string  true        "The id of session"
// @Success 200 {object} object.Session
// @router /get-session [get]
func (c *ApiController) GetConnSession() {
	id := c.Input().Get("id")

	session, err := object.GetConnSession(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(session)
}

// DeleteSession
// @Title DeleteSession
// @Tag Session API
// @Description delete session
// @Param   id     query    string  true        "The id of session"
// @Success 200 {object} Response
// @router /delete-session [post]
func (c *ApiController) DeleteSession() {
	id := c.Input().Get("id")

	affected, err := object.DeleteSession(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(affected)
}

// UpdateSession
// @Title UpdateSession
// @Tag Session API
// @Description update session
// @Param   id     query    string  true        "The id of session"
// @Param   body    body   object.Session
// @Success 200 {object} Response
// @router /update-session [post]
func (c *ApiController) UpdateSession() {
	id := c.Input().Get("id")

	var session object.Session
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &session)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateSession(id, &session))
	c.ServeJSON()
}

// AddSession
// @Title AddSession
// @Tag Session API
// @Description add session
// @Param   body    body   object.Session
// @Success 200 {object} Response
// @router /add-session [post]
func (c *ApiController) AddSession() {
	var session object.Session
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &session)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddSession(&session))
	c.ServeJSON()
}
