// Copyright 2025 The PathsCompare Authors. All Rights Reserved.
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
import {Button, Card, Col, message, Row, Upload} from "antd";
import * as Setting from "./Setting";
import i18next from "i18next";
import * as PathsCompareBackend from "./backend/PathsCompareBackend";

class PathsComparePage extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      standardBpmnFile: null,
      unknownBpmnFile: null,
    };
  }

  // 处理标准 BPMN 文件上传
  handleStandardFileChange = (info) => {
    if (info.file.status === "done") {
      this.setState({
        standardBpmnFile: info.file.originFileObj,
      });
      message.success("标准 BPMN 文件上传成功");
    } else if (info.file.status === "error") {
      message.error("标准 BPMN 文件上传失败");
    }
  };

  // 处理未知 BPMN 文件上传
  handleUnknownFileChange = (info) => {
    if (info.file.status === "done") {
      this.setState({
        unknownBpmnFile: info.file.originFileObj,
      });
      message.success("实际 BPMN 文件上传成功");
    } else if (info.file.status === "error") {
      message.error("实际 BPMN 文件上传失败");
    }
  };

  // 提交路径对比请求
  submitPathsCompare = () => {
    const {standardBpmnFile, unknownBpmnFile} = this.state;
    if (!standardBpmnFile || !unknownBpmnFile) {
      message.error(i18next.t("general:Please upload both BPMN files"));
      return;
    }

    // 传递给后端进行处理
    const formData = new FormData();
    formData.append("standardBpmn", standardBpmnFile);
    formData.append("unknownBpmn", unknownBpmnFile);

    PathsCompareBackend.compareBpmn(formData)
      .then((res) => {
        if (res.status === "ok") {
          message.success(i18next.t("general:Paths compared successfully"));
          message.info(`对比结果: ${res.result}`);
        } else {
          message.error(`失败: ${res.msg}`);
        }
      })
      .catch((error) => {
        message.error(`请求失败: ${error}`);
      });
  };

  render() {
    return (
      <div>
        <Card size="small" title={i18next.t("pathsCompare:Compare Paths")} style={{marginLeft: "5px"}} type="inner">
          <Row style={{marginTop: "10px"}}>
            <Col style={{marginTop: "5px"}} span={4}>
              {Setting.getLabel(i18next.t("general:Standard BPMN File"), i18next.t("general:Standard BPMN File - Tooltip"))}:
            </Col>
            <Col span={20}>
              <Upload
                customRequest={(options) => options.onSuccess(null, options.file)}
                showUploadList={false}
                onChange={this.handleStandardFileChange}
                accept=".bpmn"
              >
                <Button>{i18next.t("general:Upload Standard BPMN File")}</Button>
              </Upload>
            </Col>
          </Row>

          <Row style={{marginTop: "20px"}}>
            <Col style={{marginTop: "5px"}} span={4}>
              {Setting.getLabel(i18next.t("general:Unknown BPMN File"), i18next.t("general:Unknown BPMN File - Tooltip"))}:
            </Col>
            <Col span={20}>
              <Upload
                customRequest={(options) => options.onSuccess(null, options.file)}
                showUploadList={false}
                onChange={this.handleUnknownFileChange}
                accept=".bpmn"
              >
                <Button>{i18next.t("general:Upload Unknown BPMN File")}</Button>
              </Upload>
            </Col>
          </Row>
        </Card>

        <div style={{marginTop: "20px", marginLeft: "40px"}}>
          <Button size="large" type="primary" onClick={this.submitPathsCompare}>
            {i18next.t("general:Submit")}
          </Button>
        </div>
      </div>
    );
  }
}

export default PathsComparePage;
