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
import {Button, Card, Col, Input, Row} from "antd";
import * as MachineBackend from "./backend/MachineBackend";
import * as Setting from "./Setting";
import i18next from "i18next";

class MachineEditPage extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      classes: props,
      machineOwner: props.match.params.organizationName,
      machineName: props.match.params.machineName,
      machine: null,
      mode: props.location.mode !== undefined ? props.location.mode : "edit",
    };
  }

  UNSAFE_componentWillMount() {
    this.getMachine();
  }

  getMachine() {
    MachineBackend.getMachine(this.props.account.owner, this.state.machineName)
      .then((res) => {
        if (res.status === "ok") {
          this.setState({
            machine: res.data,
          });
        } else {
          Setting.showMessage("error", `Failed to get machine: ${res.msg}`);
        }
      });
  }

  parseMachineField(key, value) {
    if ([].includes(key)) {
      value = Setting.myParseInt(value);
    }
    return value;
  }

  updateMachineField(key, value) {
    value = this.parseMachineField(key, value);

    const machine = this.state.machine;
    machine[key] = value;
    this.setState({
      machine: machine,
    });
  }

  renderMachine() {
    return (
      <Card size="small" title={
        <div>
          {this.state.mode === "add" ? i18next.t("machine:New Machine") : i18next.t("machine:Edit Machine")}&nbsp;&nbsp;&nbsp;&nbsp;
          <Button onClick={() => this.submitMachineEdit(false)}>{i18next.t("general:Save")}</Button>
          <Button style={{marginLeft: "20px"}} type="primary" onClick={() => this.submitMachineEdit(true)}>{i18next.t("general:Save & Exit")}</Button>
          {this.state.mode === "add" ? <Button style={{marginLeft: "20px"}} onClick={() => this.deleteMachine()}>{i18next.t("general:Cancel")}</Button> : null}
        </div>
      } style={{marginLeft: "5px"}} type="inner">
        <Row style={{marginTop: "10px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Organization"), i18next.t("general:Organization - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.owner} onChange={e => {
              this.updateMachineField("owner", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Name"), i18next.t("general:Name - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.name} onChange={e => {
              this.updateMachineField("name", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Provider"), i18next.t("general:Provider - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.provider} onChange={e => {
              this.updateMachineField("provider", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Display name"), i18next.t("general:Display name - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.displayName} onChange={e => {
              this.updateMachineField("displayName", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Created time"), i18next.t("general:Created time - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.createdTime} onChange={e => {
              this.updateMachineField("createdTime", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Expire time"), i18next.t("general:Expire time - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.expireTime} onChange={e => {
              this.updateMachineField("expireTime", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Region"), i18next.t("general:Region - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.region} onChange={e => {
              this.updateMachineField("region", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Zone"), i18next.t("general:Zone - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.zone} onChange={e => {
              this.updateMachineField("zone", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Category"), i18next.t("general:Category - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.category} onChange={e => {
              this.updateMachineField("category", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Type"), i18next.t("general:Type - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.type} onChange={e => {
              this.updateMachineField("type", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Size"), i18next.t("general:Size - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.size} onChange={e => {
              this.updateMachineField("size", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Image"), i18next.t("general:Image - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.image} onChange={e => {
              this.updateMachineField("image", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Public IP"), i18next.t("general:Public IP - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.publicIp} onChange={e => {
              this.updateMachineField("publicIp", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Private IP"), i18next.t("general:Private IP - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.privateIp} onChange={e => {
              this.updateMachineField("privateIp", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:State"), i18next.t("general:State - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.machine.state} onChange={e => {
              this.updateMachineField("state", e.target.value);
            }} />
          </Col>
        </Row>
      </Card>
    );
  }

  submitMachineEdit(willExist) {
    const machine = Setting.deepCopy(this.state.machine);
    MachineBackend.updateMachine(this.state.machine.owner, this.state.machineName, machine)
      .then((res) => {
        if (res.status === "ok") {
          if (res.data) {
            Setting.showMessage("success", "Successfully saved");
            this.setState({
              machineName: this.state.machine.name,
            });
            if (willExist) {
              this.props.history.push("/machines");
            } else {
              this.props.history.push(`/machines/${this.state.machine.owner}/${encodeURIComponent(this.state.machine.name)}`);
            }
            // this.getMachine(true);
          } else {
            Setting.showMessage("error", "failed to save: server side failure");
            this.updateMachineField("name", this.state.machineName);
          }
        } else {
          Setting.showMessage("error", `failed to save: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `failed to save: ${error}`);
      });
  }

  deleteMachine() {
    MachineBackend.deleteMachine(this.state.machine)
      .then((res) => {
        if (res.status === "ok") {
          this.props.history.push("/machines");
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
          this.state.machine !== null ? this.renderMachine() : null
        }
        <div style={{marginTop: "20px", marginLeft: "40px"}}>
          <Button size="large" onClick={() => this.submitMachineEdit(false)}>{i18next.t("general:Save")}</Button>
          <Button style={{marginLeft: "20px"}} type="primary" size="large" onClick={() => this.submitMachineEdit(true)}>{i18next.t("general:Save & Exit")}</Button>
          {this.state.mode === "add" ? <Button style={{marginLeft: "20px"}} size="large" onClick={() => this.deleteMachine()}>{i18next.t("general:Cancel")}</Button> : null}
        </div>
      </div>
    );
  }
}

export default MachineEditPage;