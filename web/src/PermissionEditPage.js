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

import React from "react";
import {Button, Card, Col, DatePicker, Input, Row, Select, Switch} from "antd";
import dayjs from "dayjs";
import * as PermissionBackend from "./backend/PermissionBackend";
import * as AssetBackend from "./backend/AssetBackend";
import * as Setting from "./Setting";
import i18next from "i18next";

const allActions = ["Connect", "Copy", "Paste", "Upload", "Download"];

class PermissionEditPage extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      classes: props,
      permissionOwner: props.match.params.organizationName,
      permissionName: props.match.params.permissionName,
      permission: null,
      users: [],
      groups: [],
      assets: [],
      mode: props.location.mode !== undefined ? props.location.mode : "edit",
    };
  }

  UNSAFE_componentWillMount() {
    this.getPermission();
    this.getOptions();
  }

  getPermission() {
    PermissionBackend.getPermission(this.state.permissionOwner, this.state.permissionName)
      .then((res) => {
        if (res.status === "ok") {
          this.setState({
            permission: res.data,
          });
        } else {
          Setting.showMessage("error", `${i18next.t("general:Failed to get")}: ${res.msg}`);
        }
      });
  }

  getOptions() {
    PermissionBackend.getUsers()
      .then((res) => {
        if (res.status === "ok") {
          this.setState({users: res.data});
        }
      });
    PermissionBackend.getGroups()
      .then((res) => {
        if (res.status === "ok") {
          this.setState({groups: res.data});
        }
      });
    AssetBackend.getAssets(this.state.permissionOwner)
      .then((res) => {
        if (res.status === "ok") {
          this.setState({assets: res.data});
        }
      });
  }

  updatePermissionField(key, value) {
    const permission = this.state.permission;
    permission[key] = value;
    this.setState({
      permission: permission,
    });
  }

  getPrincipalOptions(principals) {
    return [{name: "*", displayName: i18next.t("permission:All")}, ...principals].map(item => {
      return Setting.getOption(item.displayName && item.displayName !== item.name ? `${item.displayName} (${item.name})` : item.name, item.name);
    });
  }

  getActionLabel(action) {
    const labels = {
      Connect: i18next.t("permission:Connect"),
      Copy: i18next.t("permission:Copy"),
      Paste: i18next.t("permission:Paste"),
      Upload: i18next.t("permission:Upload"),
      Download: i18next.t("permission:Download"),
    };
    return labels[action] ?? action;
  }

  getTagOptions() {
    const tags = [...new Set(this.state.assets.map(asset => asset.tag).filter(tag => tag))];
    return tags.map(tag => Setting.getOption(tag, tag));
  }

  renderRow(label, tooltip, content) {
    return (
      <Row style={{marginTop: "20px"}} >
        <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
          {Setting.getLabel(label, tooltip)} :
        </Col>
        <Col span={22} >
          {content}
        </Col>
      </Row>
    );
  }

  renderTimePicker(key) {
    const value = this.state.permission[key];
    return (
      <DatePicker showTime style={{width: "100%"}} value={value ? dayjs(value) : null} onChange={value => {
        this.updatePermissionField(key, value ? value.format() : "");
      }} />
    );
  }

  renderPermission() {
    const permission = this.state.permission;
    return (
      <Card size="small" title={
        <div>
          {this.state.mode === "add" ? i18next.t("permission:New Permission") : i18next.t("permission:Edit Permission")}&nbsp;&nbsp;&nbsp;&nbsp;
          <Button onClick={() => this.submitPermissionEdit(false)}>{i18next.t("general:Save")}</Button>
          <Button style={{marginLeft: "20px"}} type="primary" onClick={() => this.submitPermissionEdit(true)}>{i18next.t("general:Save & Exit")}</Button>
          {this.state.mode === "add" ? <Button style={{marginLeft: "20px"}} onClick={() => this.deletePermission()}>{i18next.t("general:Cancel")}</Button> : null}
        </div>
      } style={{marginLeft: "5px"}} type="inner">
        {this.renderRow(i18next.t("general:Organization"), i18next.t("general:Organization - Tooltip"),
          <Input value={permission.owner} disabled />
        )}
        {this.renderRow(i18next.t("general:Name"), i18next.t("general:Name - Tooltip"),
          <Input value={permission.name} onChange={e => {
            this.updatePermissionField("name", e.target.value);
          }} />
        )}
        {this.renderRow(i18next.t("general:Display name"), i18next.t("general:Display name - Tooltip"),
          <Input value={permission.displayName} onChange={e => {
            this.updatePermissionField("displayName", e.target.value);
          }} />
        )}
        {this.renderRow(i18next.t("general:Description"), i18next.t("general:Description - Tooltip"),
          <Input value={permission.description} onChange={e => {
            this.updatePermissionField("description", e.target.value);
          }} />
        )}
        {this.renderRow(i18next.t("permission:Users"), i18next.t("permission:Users - Tooltip"),
          <Select virtual={false} mode="tags" style={{width: "100%"}} value={permission.users} options={this.getPrincipalOptions(this.state.users)} onChange={value => {
            this.updatePermissionField("users", value);
          }} />
        )}
        {this.renderRow(i18next.t("permission:Groups"), i18next.t("permission:Groups - Tooltip"),
          <Select virtual={false} mode="tags" style={{width: "100%"}} value={permission.groups} options={this.getPrincipalOptions(this.state.groups)} onChange={value => {
            this.updatePermissionField("groups", value);
          }} />
        )}
        {this.renderRow(i18next.t("general:Assets"), i18next.t("permission:Assets - Tooltip"),
          <Select virtual={false} mode="multiple" style={{width: "100%"}} value={permission.assets} options={this.getPrincipalOptions(this.state.assets)} onChange={value => {
            this.updatePermissionField("assets", value);
          }} />
        )}
        {this.renderRow(i18next.t("permission:Asset tags"), i18next.t("permission:Asset tags - Tooltip"),
          <Select virtual={false} mode="tags" style={{width: "100%"}} value={permission.assetTags} options={this.getTagOptions()} onChange={value => {
            this.updatePermissionField("assetTags", value);
          }} />
        )}
        {this.renderRow(i18next.t("permission:Actions"), i18next.t("permission:Actions - Tooltip"),
          <Select virtual={false} mode="multiple" style={{width: "100%"}} value={permission.actions} options={allActions.map(action => Setting.getOption(this.getActionLabel(action), action))} onChange={value => {
            this.updatePermissionField("actions", value);
          }} />
        )}
        {this.renderRow(i18next.t("permission:Client IPs"), i18next.t("permission:Client IPs - Tooltip"),
          <Select virtual={false} mode="tags" style={{width: "100%"}} value={permission.clientIps} placeholder="192.168.1.0/24" onChange={value => {
            this.updatePermissionField("clientIps", value);
          }} />
        )}
        {this.renderRow(i18next.t("permission:Start time"), i18next.t("permission:Start time - Tooltip"), this.renderTimePicker("startTime"))}
        {this.renderRow(i18next.t("permission:End time"), i18next.t("permission:End time - Tooltip"), this.renderTimePicker("endTime"))}
        {this.renderRow(i18next.t("permission:Is enabled"), i18next.t("permission:Is enabled - Tooltip"),
          <Switch checked={permission.isEnabled} onChange={checked => {
            this.updatePermissionField("isEnabled", checked);
          }} />
        )}
      </Card>
    );
  }

  submitPermissionEdit(willExist) {
    const permission = Setting.deepCopy(this.state.permission);
    PermissionBackend.updatePermission(this.state.permissionOwner, this.state.permissionName, permission)
      .then((res) => {
        if (res.status === "ok") {
          if (res.data) {
            Setting.showMessage("success", i18next.t("general:Successfully saved"));
            this.setState({
              permissionName: this.state.permission.name,
            });
            if (willExist) {
              this.props.history.push("/permissions");
            } else {
              this.props.history.push(`/permissions/${this.state.permission.owner}/${encodeURIComponent(this.state.permission.name)}`);
            }
          } else {
            Setting.showMessage("error", i18next.t("general:Failed to save"));
            this.updatePermissionField("name", this.state.permissionName);
          }
        } else {
          Setting.showMessage("error", `${i18next.t("general:Failed to save")}: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `${i18next.t("general:Failed to save")}: ${error}`);
      });
  }

  deletePermission() {
    PermissionBackend.deletePermission(this.state.permission)
      .then((res) => {
        if (res.status === "ok") {
          this.props.history.push("/permissions");
        } else {
          Setting.showMessage("error", `${i18next.t("general:Failed to delete")}: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
      });
  }

  render() {
    return (
      <div>
        {
          this.state.permission !== null ? this.renderPermission() : null
        }
        <div style={{marginTop: "20px", marginLeft: "40px"}}>
          <Button size="large" onClick={() => this.submitPermissionEdit(false)}>{i18next.t("general:Save")}</Button>
          <Button style={{marginLeft: "20px"}} type="primary" size="large" onClick={() => this.submitPermissionEdit(true)}>{i18next.t("general:Save & Exit")}</Button>
          {this.state.mode === "add" ? <Button style={{marginLeft: "20px"}} size="large" onClick={() => this.deletePermission()}>{i18next.t("general:Cancel")}</Button> : null}
        </div>
      </div>
    );
  }
}

export default PermissionEditPage;
