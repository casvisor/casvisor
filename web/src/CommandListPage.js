// Copyright 2024 The casbin Authors. All Rights Reserved.
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
import {Button, Table} from "antd";
import BaseListPage from "./BaseListPage";
import moment from "moment";
import * as Setting from "./Setting";
import * as CommandBackend from "./backend/CommandBackend";
import i18next from "i18next";
import PopconfirmModal from "./common/modal/PopconfirmModal";

class CommandListPage extends BaseListPage {
  constructor(props) {
    super(props);
  }

  newCommand() {
    return {
      owner: this.props.account.owner,
      name: `command_${Setting.getRandomName()}`,
      createdTime: moment().format(),
      displayName: `command_${Setting.getRandomName()}`,
      Command: "",
      Assets: [],
    };
  }

  addCommand() {
    const newCommand = this.newCommand();
    CommandBackend.addCommand(newCommand)
      .then((res) => {
        if (res.status === "ok") {
          this.props.history.push({pathname: `/commands/${newCommand.owner}/${newCommand.name}`, mode: "add"});
          Setting.showMessage("success", "Command added successfully");
        } else {
          Setting.showMessage("error", `Failed to add Command: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `Command failed to add: ${error}`);
      });
  }

  deleteCommand(i) {
    CommandBackend.deleteCommand(this.state.data[i])
      .then((res) => {
        if (res.status === "ok") {
          Setting.showMessage("success", "Command deleted successfully");
          this.setState({
            data: Setting.deleteRow(this.state.data, i),
            pagination: {
              ...this.state.pagination,
              total: this.state.pagination.total - 1,
            },
          });
        } else {
          Setting.showMessage("error", `Failed to delete Command: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `Command failed to delete: ${error}`);
      });
  }

  renderTable(commands) {
    const columns = [
      {
        title: i18next.t("general:Organization"),
        dataIndex: "owner",
        key: "owner",
        width: "110px",
        sorter: true,
        ...this.getColumnSearchProps("owner"),
        render: (text, command, index) => {
          return (
            <a target="_blank" rel="noreferrer" href={Setting.getMyProfileUrl(this.props.account).replace("/account", `/organizations/${text}`)}>
              {text}
            </a>
          );
        },
      },
      {
        title: i18next.t("general:Name"),
        dataIndex: "name",
        key: "name",
        width: "120px",
        sorter: true,
        ...this.getColumnSearchProps("name"),
        render: (text, record, index) => {
          return (
            <Link to={`/commands/${record.owner}/${record.name}`}>{text}</Link>
          );
        },
      },
      {
        title: i18next.t("general:Created time"),
        dataIndex: "createdTime",
        key: "createdTime",
        width: "160px",
        sorter: (a, b) => a.createdTime.localeCompare(b.createdTime),
        render: (text, record, index) => {
          return Setting.getFormattedDate(text);
        },
      },
      {
        title: i18next.t("general:Display name"),
        dataIndex: "displayName",
        key: "displayName",
        width: "150px",
        sorter: (a, b) => a.displayName.localeCompare(b.displayName),
      },
      {
        title: i18next.t("general:Command"),
        dataIndex: "command",
        key: "command",
        width: "300px",
        sorter: (a, b) => a.command.localeCompare(b.command),
      },
      {
        title: i18next.t("general:Action"),
        dataIndex: "action",
        key: "action",
        width: "260px",
        fixed: (Setting.isMobile()) ? "false" : "right",
        render: (text, record, index) => {
          return (
            <div>
              <Button
                disabled={record.owner !== this.props.account.owner}
                style={{marginTop: "10px", marginBottom: "10px", marginRight: "10px"}}
                onClick={() => this.props.history.push(`/commands/${record.owner}/${record.name}`)}
              >{i18next.t("general:Edit")}
              </Button>
              <PopconfirmModal
                disabled={record.owner !== this.props.account.owner}
                title={i18next.t("general:Sure to delete") + `: ${record.name} ?`}
                onConfirm={() => this.deleteCommand(index)}
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
        <Table scroll={{x: "max-content"}} columns={columns} dataSource={commands} rowKey={(command) => `${command.owner}/${command.name}`} size="middle" bordered pagination={paginationProps}
          title={() => (
            <div>
              {i18next.t("general:Commands")}&nbsp;&nbsp;&nbsp;&nbsp;
              <Button type="primary" size="small" onClick={this.addCommand.bind(this)}>{i18next.t("general:Add")}</Button>
            </div>
          )}
          loading={this.state.loading}
          onChange={this.handleTableChange}
        />
      </div>
    );
  }

  fetch = (params = {}) => {
    let field = params.searchedColumn, value = params.searchText;
    const sortField = params.sortField, sortOrder = params.sortOrder;
    if (params.type) {
      field = "type";
      value = params.type;
    }
    this.setState({loading: true});
    CommandBackend.getCommands(Setting.getRequestOrganization(this.props.account), params.pagination.current, params.pagination.pageSize, field, value, sortField, sortOrder)
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

export default CommandListPage;
