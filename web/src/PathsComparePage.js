import React from "react";
import {Button, Card, Col, Input, message, Row, Upload} from "antd";
import * as Setting from "./Setting";
import i18next from "i18next";
import * as PathsCompareBackend from "./backend/PathsCompareBackend";

class PathsComparePage extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      standardBpmnFile: null,
      unknownBpmnFile: null,
      compareResult: "",  // 用来保存后端返回的路径对比结果
      patientInfo: {
        organization: "",
        name: "",
        hospital: "",
      },
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
          this.setState({compareResult: res.result});
        } else {
          message.error(`失败: ${res.msg}`);
        }
      })
      .catch((error) => {
        message.error(`请求失败: ${error}`);
      });
  };

  // 更新患者信息
  handlePatientInfoChange = (field, value) => {
    this.setState({
      patientInfo: {
        ...this.state.patientInfo,
        [field]: value,
      },
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

          {/* 患者信息 */}
          <Row style={{marginTop: "20px"}}>
            <Col span={4}>{i18next.t("general:Organization")}:</Col>
            <Col span={20}>
              <Input
                value={this.state.patientInfo.organization}
                onChange={(e) => this.handlePatientInfoChange("organization", e.target.value)}
                placeholder={i18next.t("general:Enter Organization")}
              />
            </Col>
          </Row>
          <Row style={{marginTop: "10px"}}>
            <Col span={4}>{i18next.t("general:Name")}:</Col>
            <Col span={20}>
              <Input
                value={this.state.patientInfo.name}
                onChange={(e) => this.handlePatientInfoChange("name", e.target.value)}
                placeholder={i18next.t("general:Enter Name")}
              />
            </Col>
          </Row>
          <Row style={{marginTop: "10px"}}>
            <Col span={4}>{i18next.t("general:Hospital")}:</Col>
            <Col span={20}>
              <Input
                value={this.state.patientInfo.hospital}
                onChange={(e) => this.handlePatientInfoChange("hospital", e.target.value)}
                placeholder={i18next.t("general:Enter Hospital")}
              />
            </Col>
          </Row>
        </Card>

        <div style={{marginTop: "20px", marginLeft: "40px"}}>
          <Button size="large" type="primary" onClick={this.submitPathsCompare}>
            {i18next.t("general:Submit")}
          </Button>
        </div>

        {/* 结果显示框 */}
        <Card
          title={i18next.t("pathsCompare:Comparison Result")}
          style={{marginTop: "20px", marginLeft: "5px"}}
          type="inner"
        >
          <div
            style={{
              maxHeight: "300px", // 限制最大高度
              overflowY: "auto",  // 启用滚动条
              width: "50%",
              backgroundColor: "#000",  // 黑色背景
              color: "#fff",  // 白色文字
              padding: "10px",
              fontFamily: "monospace",  // 等宽字体
              whiteSpace: "pre-wrap",   // 保留换行
            }}
          >
            {this.state.compareResult || "对比结果"}
          </div>
        </Card>
      </div>
    );
  }
}

export default PathsComparePage;
