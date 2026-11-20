// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/beego/beego/utils/pagination"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const (
	pendingLoginReviewSessionKey = "pendingLoginReview"
	pendingLoginClaimsSessionKey = "pendingLoginClaims"
	MfaSetupRequiredSessionKey   = "mfaSetupRequired"
)

// GetLoginRules
// @Title GetLoginRules
// @Tag Login Rule API
// @Description get the login rules, by priority
// @Param   owner     query    string  true        "The owner of the login rules"
// @Success 200 {array} object.LoginRule The Response object
// @router /get-login-rules [get]
func (c *ApiController) GetLoginRules() {
	owner := c.Input().Get("owner")
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		rules, err := object.GetLoginRules(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(rules)
		return
	}

	count, err := object.GetLoginRuleCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, util.ParseInt(limit), count)
	rules, err := object.GetPaginationLoginRules(owner, paginator.Offset(), util.ParseInt(limit), field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(rules, paginator.Nums())
}

// GetLoginRule
// @Title GetLoginRule
// @Tag Login Rule API
// @Description get a login rule
// @Param   id     query    string  true        "The id ( owner/name ) of the login rule"
// @Success 200 {object} object.LoginRule The Response object
// @router /get-login-rule [get]
func (c *ApiController) GetLoginRule() {
	rule, err := object.GetLoginRule(c.Input().Get("id"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(rule)
}

// UpdateLoginRule
// @Title UpdateLoginRule
// @Tag Login Rule API
// @Description update a login rule
// @Param   id     query    string  true        "The id ( owner/name ) of the login rule"
// @Param   body    body   object.LoginRule  true        "The details of the login rule"
// @Success 200 {object} controllers.Response The Response object
// @router /update-login-rule [post]
func (c *ApiController) UpdateLoginRule() {
	var rule object.LoginRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateLoginRule(c.Input().Get("id"), &rule))
	c.ServeJSON()
}

// AddLoginRule
// @Title AddLoginRule
// @Tag Login Rule API
// @Description add a login rule
// @Param   body    body   object.LoginRule  true        "The details of the login rule"
// @Success 200 {object} controllers.Response The Response object
// @router /add-login-rule [post]
func (c *ApiController) AddLoginRule() {
	var rule object.LoginRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddLoginRule(&rule))
	c.ServeJSON()
}

// DeleteLoginRule
// @Title DeleteLoginRule
// @Tag Login Rule API
// @Description delete a login rule
// @Param   body    body   object.LoginRule  true        "The login rule"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-login-rule [post]
func (c *ApiController) DeleteLoginRule() {
	var rule object.LoginRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteLoginRule(&rule))
	c.ServeJSON()
}

// GetLoginReviews
// @Title GetLoginReviews
// @Tag Login Rule API
// @Description get the sign-ins held for review
// @Param   owner     query    string  true        "The owner of the reviews"
// @Param   status    query    string  false       "Pending, Approved, Rejected, Canceled, Expired or Used"
// @Success 200 {array} object.LoginReview The Response object
// @router /get-login-reviews [get]
func (c *ApiController) GetLoginReviews() {
	owner := c.Input().Get("owner")
	status := c.Input().Get("status")
	limit, err := strconv.Atoi(c.Input().Get("pageSize"))
	if err != nil || limit <= 0 {
		limit = 100
	}

	count, err := object.GetLoginReviewCount(owner, status, c.Input().Get("field"), c.Input().Get("value"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	reviews, err := object.GetPaginationLoginReviews(owner, status, paginator.Offset(), limit, c.Input().Get("field"), c.Input().Get("value"), c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(reviews, paginator.Nums())
}

type reviewLoginForm struct {
	Id      string `json:"id"`
	Approve bool   `json:"approve"`
	Comment string `json:"comment"`
}

// ReviewLogin
// @Title ReviewLogin
// @Tag Login Rule API
// @Description approve or reject a sign-in held for review
// @Param   body    body   controllers.reviewLoginForm  true        "The review id, approve or not, and a comment"
// @Success 200 {object} object.LoginReview The Response object
// @router /review-login [post]
func (c *ApiController) ReviewLogin() {
	var form reviewLoginForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	user := c.GetSessionUser()
	review, err := object.ReviewLogin(form.Id, user, form.Approve, form.Comment)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	action := "login-review-reject"
	if form.Approve {
		action = "login-review-approve"
	}
	c.addLoginAudit(user.Owner, user.Name, action, fmt.Sprintf("Login review %s of %s from %s (%s): %s", review.Name, review.User, review.ClientIp, review.Method, form.Comment))
	c.ResponseOk(review)
}

// GetLoginReviewStatus
// @Title GetLoginReviewStatus
// @Tag Login Rule API
// @Description tell the browser whose sign-in is held for review how the review stands, and sign it in once approved
// @Success 200 {object} controllers.Response The Response object
// @router /get-login-review-status [get]
func (c *ApiController) GetLoginReviewStatus() {
	id, _ := c.GetSession(pendingLoginReviewSessionKey).(string)
	if id == "" {
		c.ResponseError("no sign-in is waiting for review")
		return
	}

	review, err := object.GetLoginReview(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if review == nil {
		c.clearPendingLogin()
		c.ResponseError("no sign-in is waiting for review")
		return
	}

	res := map[string]string{"status": review.Status, "rule": review.Rule, "reviewer": review.Reviewer, "comment": review.Comment, "expireTime": review.ExpireTime}
	switch review.Status {
	case object.LoginReviewPending:
		c.ResponseOk(res)
	case object.LoginReviewApproved:
		claims, ok := c.GetSession(pendingLoginClaimsSessionKey).(casdoorsdk.Claims)
		used, err := object.ConsumeLoginReview(id)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.clearPendingLogin()
		if !ok || !used {
			c.ResponseError("the approval has already been used")
			return
		}
		c.SessionRegenerateID()
		c.SetSessionClaims(&claims)
		c.Ctx.Input.SetParam("recordUserId", claims.User.Owner+"/"+claims.User.Name)
		res["status"] = object.LoginReviewUsed
		c.ResponseOk(res)
	default:
		c.clearPendingLogin()
		c.ResponseOk(res)
	}
}

// CancelLoginReview
// @Title CancelLoginReview
// @Tag Login Rule API
// @Description stop waiting for the review of this browser's sign-in
// @Success 200 {object} controllers.Response The Response object
// @router /cancel-login-review [post]
func (c *ApiController) CancelLoginReview() {
	id, _ := c.GetSession(pendingLoginReviewSessionKey).(string)
	if id != "" {
		err := object.CancelLoginReview(id)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
	}
	c.clearPendingLogin()
	c.ResponseOk()
}

func (c *ApiController) clearPendingLogin() {
	c.DelSession(pendingLoginReviewSessionKey)
	c.DelSession(pendingLoginClaimsSessionKey)
}

func (c *ApiController) addLoginAudit(organization string, userName string, action string, detail string) {
	object.AddAuditRecord(&object.AuditEvent{
		Organization: organization,
		User:         userName,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "Login",
		Action:       action,
		Detail:       detail,
	})
}

// checkLoginRules applies the login rules to a web sign-in whose credentials are right. It answers the
// request itself and returns false when the sign-in is rejected or held for review; mfaSetup tells that
// the user has to turn on two-step verification before doing anything else.
func (c *ApiController) checkLoginRules(claims *casdoorsdk.Claims, mfaEnabled bool) (ok bool, mfaSetup bool) {
	user := &claims.User
	decision, err := object.CheckLoginRules(user, c.getAuthzClientIp(), object.LoginMethodWeb, time.Now())
	if err != nil {
		c.ResponseError(err.Error())
		return false, false
	}

	c.DelSession(MfaSetupRequiredSessionKey)
	c.clearPendingLogin()

	switch decision.Action {
	case object.LoginActionReject:
		c.addLoginAudit(user.Owner, user.Name, "login-reject", fmt.Sprintf("Sign-in rejected by the login rule: %s", decision.RuleName()))
		c.ResponseError(fmt.Sprintf("the sign-in is not allowed by the login rule: %s", decision.RuleName()), "login-rejected")
		return false, false
	case object.LoginActionReview:
		review, err := object.AddLoginReview(user, decision, c.getAuthzClientIp(), c.getUserAgent(), object.LoginMethodWeb)
		if err != nil {
			c.ResponseError(err.Error())
			return false, false
		}
		c.SessionRegenerateID()
		c.SetSession(pendingLoginReviewSessionKey, review.GetId())
		c.SetSession(pendingLoginClaimsSessionKey, *claims)
		c.addLoginAudit(user.Owner, user.Name, "login-review", fmt.Sprintf("Sign-in held for review %s by the login rule: %s", review.Name, decision.RuleName()))
		c.ResponseError("the sign-in is waiting for an administrator to approve it", "login-review", map[string]string{"rule": decision.RuleName(), "expireTime": review.ExpireTime})
		return false, false
	case object.LoginActionMfa:
		if object.IsLocalAuth() && !mfaEnabled {
			c.addLoginAudit(user.Owner, user.Name, "login-mfa-setup", fmt.Sprintf("Two-step verification required by the login rule: %s", decision.RuleName()))
			return true, true
		}
	}
	return true, false
}
