/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useRef, useState } from 'react';
import { Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const DEFAULT_CONTENT_REVIEW_INPUTS = {
  'internal_review.enabled': false,
  'internal_review.endpoint': '',
  'internal_review.bearer_token': '',
  'internal_review.timeout_seconds': 10,
  'internal_review.fail_closed': true,
  'internal_review.scope': 'text',
  'internal_review.model_filter': '',
};

export default function SettingContentReview(props) {
  const { t } = useTranslation();

  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    ...DEFAULT_CONTENT_REVIEW_INPUTS,
  });
  const [inputsRow, setInputsRow] = useState({
    ...DEFAULT_CONTENT_REVIEW_INPUTS,
  });
  const refForm = useRef();

  const toOptionInputs = (values) => {
    const modelFilter = String(values['internal_review.model_filter'] || '')
      .split(/[\n,]+/)
      .map((item) => item.trim())
      .filter(Boolean)
      .join(',');

    return {
      'internal_review.enabled': values['internal_review.enabled'],
      'internal_review.endpoint': String(
        values['internal_review.endpoint'] || '',
      ).trim(),
      'internal_review.bearer_token': String(
        values['internal_review.bearer_token'] || '',
      ).trim(),
      'internal_review.timeout_seconds': Number(
        values['internal_review.timeout_seconds'] || 10,
      ),
      'internal_review.fail_closed': values['internal_review.fail_closed'],
      'internal_review.scope': 'text',
      'internal_review.model_filter': modelFilter,
    };
  };

  async function onSubmit() {
    await refForm.current
      .validate()
      .then(() => {
        const currentOptions = toOptionInputs(inputs);
        const originalOptions = toOptionInputs(inputsRow);
        const updateArray = compareObjects(currentOptions, originalOptions);
        if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));

        const requestQueue = updateArray.map((item) =>
          API.put('/api/option/', {
            key: item.key,
            value: String(currentOptions[item.key]),
          }),
        );

        setLoading(true);
        Promise.all(requestQueue)
          .then((res) => {
            if (res.includes(undefined)) return showError(t('部分保存失败，请重试'));
            showSuccess(t('保存成功'));
            props.refresh();
          })
          .catch(() => {
            showError(t('保存失败，请重试'));
          })
          .finally(() => {
            setLoading(false);
          });
      })
      .catch((error) => {
        console.error('Validation failed:', error);
        showError(t('请检查输入'));
      });
  }

  useEffect(() => {
    const currentInputs = { ...DEFAULT_CONTENT_REVIEW_INPUTS };
    for (const key of Object.keys(DEFAULT_CONTENT_REVIEW_INPUTS)) {
      if (props.options[key] !== undefined) {
        currentInputs[key] = props.options[key];
      }
    }
    const formInputs = { ...currentInputs, 'internal_review.scope': 'text' };

    setInputs(formInputs);
    setInputsRow(structuredClone(formInputs));
    if (refForm.current) {
      refForm.current.setValues(formInputs);
    }
  }, [props.options]);

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('内容审核')}>
          <Row>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                label={t('启用内容审核')}
                field={'internal_review.enabled'}
                onChange={(value) =>
                  setInputs({ ...inputs, 'internal_review.enabled': value })
                }
                extraText={t('请求会在预扣费后、转发上游模型前进行内容审核。')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                label={t('审核异常时拦截')}
                field={'internal_review.fail_closed'}
                onChange={(value) =>
                  setInputs({
                    ...inputs,
                    'internal_review.fail_closed': value,
                  })
                }
                extraText={t('开启后，审核超时或审核服务异常会拦截请求。')}
              />
            </Col>
          </Row>

          <Row>
            <Col xs={24} sm={16} md={12} lg={12} xl={12}>
              <Form.Input
                label={t('审核接口地址')}
                field={'internal_review.endpoint'}
                placeholder='https://review.example.com/review'
                onChange={(value) =>
                  setInputs({ ...inputs, 'internal_review.endpoint': value })
                }
                rules={[
                  {
                    validator: (rule, value) => {
                      if (!value) return true;
                      try {
                        const url = new URL(value);
                        return url.protocol === 'http:' || url.protocol === 'https:';
                      } catch {
                        return false;
                      }
                    },
                    message: t('请输入有效的 HTTP/HTTPS 地址'),
                  },
                ]}
                extraText={t(
                  'NewAPI 会发送 POST 请求，包含模型、文本、请求体和元数据。',
                )}
              />
            </Col>
            <Col xs={24} sm={8} md={6} lg={6} xl={6}>
              <Form.Input
                label={t('Bearer Token')}
                field={'internal_review.bearer_token'}
                mode='password'
                onChange={(value) =>
                  setInputs({
                    ...inputs,
                    'internal_review.bearer_token': value,
                  })
                }
                extraText={t('不用鉴权可留空。')}
              />
            </Col>
            <Col xs={24} sm={8} md={6} lg={6} xl={6}>
              <Form.InputNumber
                label={t('审核超时（秒）')}
                field={'internal_review.timeout_seconds'}
                min={1}
                max={120}
                step={1}
                precision={0}
                onChange={(value) =>
                  setInputs({
                    ...inputs,
                    'internal_review.timeout_seconds': value,
                  })
                }
                extraText={t('等待内容审核服务返回的最长时间。')}
              />
            </Col>
          </Row>

          <Row>
            <Col span={24}>
              <div style={{ marginBottom: 16, color: 'var(--semi-color-text-2)' }}>
                {t('审核范围')}：{t('仅审核 Chat、Responses、Claude、Gemini 等文本请求。')}
              </div>
            </Col>
          </Row>

          <Row>
            <Col span={24}>
              <Form.TextArea
                label={t('模型过滤')}
                field={'internal_review.model_filter'}
                placeholder='gpt-4o,claude,!test-model'
                rows={4}
                onChange={(value) =>
                  setInputs({
                    ...inputs,
                    'internal_review.model_filter': value,
                  })
                }
                extraText={t(
                  '可选，逗号或换行分隔模型关键词；前缀 ! 表示排除，留空表示所有模型。',
                )}
              />
            </Col>
          </Row>

          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存')}
            </Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
