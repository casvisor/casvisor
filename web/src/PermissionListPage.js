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
import {Link} from "react-router-dom";
import {Button, Switch, Table, Tag} from "antd";
import BaseListPage from "./BaseListPage";
import moment from "moment";
import * as Setting from "./Setting";
import * as PermissionBackend from "./backend/PermissionBackend";
import i18next from "i18next";
import PopconfirmModal from "./common/modal/PopconfirmModal";

class PermissionListPage extends BaseListPage {
  newPermission() {
    return {
      owner: this.props.account.owner,
      name: `permission_${Setting.getRandomName()}`,
      createdTime: moment().format(),
      updatedTime: moment().format(),
      displayName: `New Permission - ${Setting.getRandomName()}`,
      description: "",
      users: [],
      groups: [],
      assets: [],
      assetTags: [],
      actions: ["Connect", "Copy", "Paste"],
      clientIps: [],
      startTime: "",
      endTime: "",
      isEnabled: true,
    };
  }

  addPermission() {
    const newPermission = this.newPermission();
    PermissionBackend.addPermission(newPermission)
      .then((res) => {
        if (res.status === "ok") {
          this.props.history.push({pathname: `/permissions/${newPermission.owner}/${newPermission.name}`, mode: "add"});
          Setting.showMessage("success", i18next.t("general:Successfully added"));
        } else {
          Setting.showMessage("error", `${i18next.t("general:Failed to add")}: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
      });
  }

  deletePermission(i) {
    PermissionBackend.deletePermission(this.state.data[i])
      .then((res) => {
        if (res.status === "ok") {
          Setting.showMessage("success", i18next.t("general:Successfully deleted"));
          this.setState({
            data: Setting.deleteRow(this.state.data, i),
            pagination: {
              ...this.state.pagination,
              total: this.state.pagination.total - 1,
            },
          });
        } else {
          Setting.showMessage("error", `${i18next.t("general:Failed to delete")}: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
      });
  }

  renderTags(items) {
    if (!items) {
      return null;
    }
    return items.map(item => <Tag key={item}>{item}</Tag>);
  }

  renderTable(permissions) {
    const columns = [
      {
        title: i18next.t("general:Name"),
        dataIndex: "name",
        key: "name",
        width: "150px",
        fixed: "left",
        sorter: true,
        ...this.getColumnSearchProps("name"),
        render: (text, record, index) => {
          return (
            <Link to={`/permissions/${record.owner}/${record.name}`}>{text}</Link>
          );
        },
      },
      {
        title: i18next.t("general:Display name"),
        dataIndex: "displayName",
        key: "displayName",
        width: "160px",
        sorter: true,
        ...this.getColumnSearchProps("displayName"),
      },
      {
        title: i18next.t("permission:Users"),
        dataIndex: "users",
        key: "users",
        render: (text, record, index) => this.renderTags(text),
      },
      {
        title: i18next.t("permission:Groups"),
        dataIndex: "groups",
        key: "groups",
        render: (text, record, index) => this.renderTags(text),
      },
      {
        title: i18next.t("general:Assets"),
        dataIndex: "assets",
        key: "assets",
        render: (text, record, index) => this.renderTags(text),
      },
      {
        title: i18next.t("permission:Asset tags"),
        dataIndex: "assetTags",
        key: "assetTags",
        render: (text, record, index) => this.renderTags(text),
      },
      {
        title: i18next.t("permission:Actions"),
        dataIndex: "actions",
        key: "actions",
        render: (text, record, index) => this.renderTags(text),
      },
      {
        title: i18next.t("permission:Client IPs"),
        dataIndex: "clientIps",
        key: "clientIps",
        render: (text, record, index) => this.renderTags(text),
      },
      {
        title: i18next.t("permission:End time"),
        dataIndex: "endTime",
        key: "endTime",
        width: "160px",
        sorter: true,
        render: (text, record, index) => {
          return text === "" ? i18next.t("permission:Never") : Setting.getFormattedDate(text);
        },
      },
      {
        title: i18next.t("permission:Is enabled"),
        dataIndex: "isEnabled",
        key: "isEnabled",
        width: "100px",
        render: (text, record, index) => {
          return (
            <Switch disabled checked={text} />
          );
        },
      },
      {
        title: i18next.t("general:Action"),
        dataIndex: "action",
        key: "action",
        width: "170px",
        fixed: (Setting.isMobile()) ? "false" : "right",
        render: (text, permission, index) => {
          return (
            <div>
              <Button
                style={{marginTop: "10px", marginBottom: "10px", marginRight: "10px"}}
                onClick={() => this.props.history.push(`/permissions/${permission.owner}/${permission.name}`)}
              >{i18next.t("general:Edit")}
              </Button>
              <PopconfirmModal
                title={i18next.t("general:Sure to delete") + `: ${permission.name} ?`}
                onConfirm={() => this.deletePermission(index)}
              >
              </PopconfirmModal>
            </div>
          );
        },
      },
    ];

    const paginationProps = {
      pageSize: this.state.pagination.pageSize,
      total: this.state.pagination.total,
      showQuickJumper: true,
      showSizeChanger: true,
      showTotal: () => i18next.t("general:{total} in total").replace("{total}", this.state.pagination.total),
    };

    return (
      <div>
        <Table scroll={{x: "max-content"}} columns={columns} dataSource={permissions} rowKey={(permission) => `${permission.owner}/${permission.name}`} size="middle" bordered pagination={paginationProps}
          title={() => (
            <div>
              {i18next.t("general:Permissions")}&nbsp;&nbsp;&nbsp;&nbsp;
              <Button type="primary" size="small" onClick={this.addPermission.bind(this)}>{i18next.t("general:Add")}</Button>
            </div>
          )}
          loading={this.state.loading}
          onChange={this.handleTableChange}
        />
      </div>
    );
  }

  fetch = (params = {}) => {
    const field = params.searchedColumn, value = params.searchText;
    const sortField = params.sortField, sortOrder = params.sortOrder;
    this.setState({loading: true});
    PermissionBackend.getPermissions(Setting.getRequestOrganization(this.props.account), params.pagination.current, params.pagination.pageSize, field, value, sortField, sortOrder)
      .then((res) => {
        this.setState({
          loading: false,
        });
        if (res.status === "ok") {
          this.setState({
            data: res.data,
            pagination: {
              ...params.pagination,
              total: res.data2,
            },
            searchText: params.searchText,
            searchedColumn: params.searchedColumn,
          });
        } else {
          if (Setting.isResponseDenied(res)) {
            this.setState({
              isAuthorized: false,
            });
          } else {
            Setting.showMessage("error", res.msg);
          }
        }
      });
  };
}

export default PermissionListPage;
