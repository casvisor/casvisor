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
import {Button, Card, Col, Input, Row, Select} from "antd";
import {LinkOutlined} from "@ant-design/icons";
import * as DoctorBackend from "./backend/DoctorBackend";
import * as Setting from "./Setting";
import i18next from "i18next";

class DoctorEditPage extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      classes: props,
      doctorOwner: props.match.params.organizationName,
      doctorName: props.match.params.doctorName,
      doctor: null,
      mode: props.location.mode !== undefined ? props.location.mode : "edit",
    };
  }

  UNSAFE_componentWillMount() {
    this.getDoctor();
  }

  getDoctor() {
    DoctorBackend.getDoctor(this.props.account.owner, this.state.doctorName)
      .then((res) => {
        if (res.status === "ok") {
          this.setState({
            doctor: res.data,
          });
        } else {
          Setting.showMessage("error", `Failed to get doctor: ${res.msg}`);
        }
      });
  }

  parseDoctorField(key, value) {
    if ([].includes(key)) {
      value = Setting.myParseInt(value);
    }
    return value;
  }

  updateDoctorField(key, value) {
    value = this.parseDoctorField(key, value);

    const doctor = this.state.doctor;
    doctor[key] = value;
    this.setState({
      doctor: doctor,
    });
  }

  renderDoctor() {
    return (
      <Card size="small" title={
        <div>
          {this.state.mode === "add" ? i18next.t("doctor:New Doctor") : i18next.t("doctor:Edit Doctor")}&nbsp;&nbsp;&nbsp;&nbsp;
          <Button onClick={() => this.submitDoctorEdit(false)}>{i18next.t("general:Save")}</Button>
          <Button style={{marginLeft: "20px"}} type="primary" onClick={() => this.submitDoctorEdit(true)}>{i18next.t("general:Save & Exit")}</Button>
          {this.state.mode === "add" ? <Button style={{marginLeft: "20px"}} onClick={() => this.deleteDoctor()}>{i18next.t("general:Cancel")}</Button> : null}
        </div>
      } style={{marginLeft: "5px"}} type="inner">
        <Row style={{marginTop: "10px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Organization"), i18next.t("general:Organization - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.doctor.owner} onChange={e => {
              this.updateDoctorField("owner", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Name"), i18next.t("general:Name - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.doctor.name} onChange={e => {
              this.updateDoctorField("name", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}}>
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("doctor:Category"), i18next.t("doctor:Category - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Select virtual={false} style={{width: "100%"}} value={this.state.doctor.category} onChange={value => {
              this.updateDoctorField("category", value);
              if (value === "Public Cloud") {
                this.updateDoctorField("type", "Amazon Web Services");
              } else if (value === "Private Cloud") {
                this.updateDoctorField("type", "KVM");
              } else if (value === "Blockchain") {
                this.updateDoctorField("type", "Hyperledger Fabric");
              }
            }}
            options={[
              {value: "Public Cloud", label: "Public Cloud"},
              {value: "Private Cloud", label: "Private Cloud"},
              {value: "Blockchain", label: "Blockchain"},
            ].map(item => Setting.getOption(item.label, item.value))} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Client ID"), i18next.t("general:Client ID - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.doctor.clientId} onChange={e => {
              this.updateDoctorField("clientId", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Client secret"), i18next.t("general:Client secret - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.doctor.clientSecret} onChange={e => {
              this.updateDoctorField("clientSecret", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("general:Region"), i18next.t("general:Region - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input value={this.state.doctor.region} onChange={e => {
              this.updateDoctorField("region", e.target.value);
            }} />
          </Col>
        </Row>
        {
          this.state.doctor.category !== "Blockchain" ? null : (
            <React.Fragment>
              <Row style={{marginTop: "20px"}} >
                <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
                  {Setting.getLabel(i18next.t("general:Network"), i18next.t("general:Network - Tooltip"))} :
                </Col>
                <Col span={22} >
                  <Input value={this.state.doctor.network} onChange={e => {
                    this.updateDoctorField("network", e.target.value);
                  }} />
                </Col>
              </Row>
              <Row style={{marginTop: "20px"}} >
                <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
                  {Setting.getLabel(i18next.t("general:Chain"), i18next.t("general:Chain - Tooltip"))} :
                </Col>
                <Col span={22} >
                  <Input value={this.state.doctor.chain} onChange={e => {
                    this.updateDoctorField("chain", e.target.value);
                  }} />
                </Col>
              </Row>
            </React.Fragment>
          )
        }
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("doctor:Browser URL"), i18next.t("doctor:Browser URL - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input prefix={<LinkOutlined />} value={this.state.doctor.browserUrl} onChange={e => {
              this.updateDoctorField("browserUrl", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}} >
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("doctor:Doctor URL"), i18next.t("doctor:Doctor URL - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Input prefix={<LinkOutlined />} value={this.state.doctor.doctorUrl} onChange={e => {
              this.updateDoctorField("doctorUrl", e.target.value);
            }} />
          </Col>
        </Row>
        <Row style={{marginTop: "20px"}}>
          <Col style={{marginTop: "5px"}} span={(Setting.isMobile()) ? 22 : 2}>
            {Setting.getLabel(i18next.t("doctor:State"), i18next.t("doctor:State - Tooltip"))} :
          </Col>
          <Col span={22} >
            <Select virtual={false} style={{width: "100%"}} value={this.state.doctor.state} onChange={value => {
              this.updateDoctorField("state", value);
            }}
            options={[
              {value: "Active", label: "Active"},
              {value: "Inactive", label: "Inactive"},
            ].map(item => Setting.getOption(item.label, item.value))} />
          </Col>
        </Row>
      </Card>
    );
  }

  submitDoctorEdit(willExist) {
    const doctor = Setting.deepCopy(this.state.doctor);
    DoctorBackend.updateDoctor(this.state.doctor.owner, this.state.doctorName, doctor)
      .then((res) => {
        if (res.status === "ok") {
          if (res.data) {
            Setting.showMessage("success", "Successfully saved");
            this.setState({
              doctorName: this.state.doctor.name,
            });
            if (willExist) {
              this.props.history.push("/doctors");
            } else {
              this.props.history.push(`/doctors/${this.state.doctor.owner}/${encodeURIComponent(this.state.doctor.name)}`);
            }
            // this.getDoctor(true);
          } else {
            Setting.showMessage("error", "failed to save: server side failure");
            this.updateDoctorField("name", this.state.doctorName);
          }
        } else {
          Setting.showMessage("error", `failed to save: ${res.msg}`);
        }
      })
      .catch(error => {
        Setting.showMessage("error", `failed to save: ${error}`);
      });
  }

  deleteDoctor() {
    DoctorBackend.deleteDoctor(this.state.doctor)
      .then((res) => {
        if (res.status === "ok") {
          this.props.history.push("/doctors");
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
          this.state.doctor !== null ? this.renderDoctor() : null
        }
        <div style={{marginTop: "20px", marginLeft: "40px"}}>
          <Button size="large" onClick={() => this.submitDoctorEdit(false)}>{i18next.t("general:Save")}</Button>
          <Button style={{marginLeft: "20px"}} type="primary" size="large" onClick={() => this.submitDoctorEdit(true)}>{i18next.t("general:Save & Exit")}</Button>
          {this.state.mode === "add" ? <Button style={{marginLeft: "20px"}} size="large" onClick={() => this.deleteDoctor()}>{i18next.t("general:Cancel")}</Button> : null}
        </div>
      </div>
    );
  }
}

export default DoctorEditPage;
