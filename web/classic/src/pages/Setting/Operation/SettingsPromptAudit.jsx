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
import {
  Button,
  Checkbox,
  Col,
  Form,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const scannerOptions = [
  ['violent', '暴力'],
  ['non_violent_illegal_acts', '非暴力违法行为'],
  ['sexual_content_or_sexual_acts', '性内容或性行为'],
  ['pii', '个人敏感信息'],
  ['suicide_and_self_harm', '自杀与自残'],
  ['unethical_acts', '不道德行为'],
  ['politically_sensitive_topics', '政治敏感话题'],
  ['copyright_violation', '版权侵权'],
  ['jailbreak', '越狱与提示注入'],
];

const defaultScanners = scannerOptions.map(([id]) => id).join(',');
const scannerIDs = new Set(scannerOptions.map(([id]) => id));

function parseGroupPolicies(value) {
  try {
    const parsed = JSON.parse(value || '{}');
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return [];
    }
    return Object.entries(parsed)
      .map(([group, policy]) => ({
        group,
        enabled: policy?.enabled === true,
        failClosed: policy?.fail_closed === true,
        scanners: Array.isArray(policy?.scanners)
          ? policy.scanners.filter((scanner) => scannerIDs.has(scanner))
          : [],
      }))
      .filter((policy) => policy.group)
      .sort((left, right) => left.group.localeCompare(right.group));
  } catch {
    return [];
  }
}

function serializeGroupPolicies(policies) {
  const serialized = {};
  [...policies]
    .sort((left, right) => left.group.localeCompare(right.group))
    .forEach((policy) => {
      serialized[policy.group.trim()] = {
        enabled: policy.enabled,
        fail_closed: policy.failClosed,
        scanners: policy.scanners,
      };
    });
  return JSON.stringify(serialized);
}

function getAvailableGroups(groupRatio, policies) {
  const groups = new Set(policies.map((policy) => policy.group));
  try {
    const parsed = JSON.parse(groupRatio || '{}');
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      Object.keys(parsed).forEach((group) => groups.add(group));
    }
  } catch {
    // Existing policies remain editable when GroupRatio is temporarily invalid.
  }
  return [...groups]
    .filter(Boolean)
    .sort((left, right) => left.localeCompare(right));
}

const initialInputs = {
  PromptAuditBaseURL: 'http://qwen3guard:11434',
  PromptAuditModel: 'sileader/qwen3guard:0.6b',
  PromptAuditAPIKey: '',
  PromptAuditTimeoutMS: 10000,
  PromptAuditInputLimit: 8000,
  PromptAuditMaxConcurrency: 4,
  PromptAuditScanners: defaultScanners,
  PromptAuditGroupPolicies: '{}',
  PromptAuditFailClosed: false,
  PromptAuditEnabled: false,
  PromptAuditKeyConfigured: false,
};

export default function SettingsPromptAudit(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [inputs, setInputs] = useState(initialInputs);
  const [inputsRow, setInputsRow] = useState(initialInputs);
  const refForm = useRef();

  function validateInputs() {
    try {
      const endpoint = new URL(inputs.PromptAuditBaseURL);
      if (
        !['http:', 'https:'].includes(endpoint.protocol) ||
        endpoint.username ||
        endpoint.password ||
        endpoint.search ||
        endpoint.hash
      ) {
        throw new Error('invalid endpoint');
      }
    } catch {
      showError(t('请填写有效的审核后端 Base URL'));
      return false;
    }
    if (!inputs.PromptAuditModel.trim()) {
      showError(t('请填写审核模型'));
      return false;
    }
    if (!inputs.PromptAuditScanners.trim()) {
      showError(t('请至少启用一类审核规则'));
      return false;
    }
    const policies = parseGroupPolicies(inputs.PromptAuditGroupPolicies);
    if (
      policies.some(
        (policy) => !policy.group.trim() || policy.scanners.length === 0,
      )
    ) {
      showError(t('请完善分组审核策略'));
      return false;
    }
    return true;
  }

  async function onSubmit() {
    if (!validateInputs()) return;
    const updateArray = compareObjects(inputs, inputsRow).filter(
      (item) =>
        item.key !== 'PromptAuditKeyConfigured' &&
        !(item.key === 'PromptAuditAPIKey' && inputs.PromptAuditAPIKey === ''),
    );
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));

    const enabledUpdate = updateArray.find(
      (item) => item.key === 'PromptAuditEnabled',
    );
    const orderedUpdates = updateArray.filter(
      (item) => item.key !== 'PromptAuditEnabled',
    );
    if (enabledUpdate) orderedUpdates.push(enabledUpdate);

    setLoading(true);
    try {
      for (const item of orderedUpdates) {
        const value =
          typeof inputs[item.key] === 'boolean'
            ? String(inputs[item.key])
            : inputs[item.key];
        const response = await API.put('/api/option/', {
          key: item.key,
          value,
        });
        if (response === undefined || !response.data?.success) {
          throw new Error(response?.data?.message || 'option update failed');
        }
      }
      showSuccess(t('保存成功'));
      props.refresh();
    } catch (error) {
      showError(error instanceof Error ? error.message : t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  }

  async function testConnection() {
    if (!validateInputs()) return;
    setTesting(true);
    try {
      const response = await API.post('/api/option/prompt_audit/test', {
        base_url: inputs.PromptAuditBaseURL,
        model: inputs.PromptAuditModel,
        api_key: inputs.PromptAuditAPIKey.trim(),
        timeout_ms: inputs.PromptAuditTimeoutMS,
        input_limit: inputs.PromptAuditInputLimit,
        max_concurrency: inputs.PromptAuditMaxConcurrency,
        scanners: inputs.PromptAuditScanners,
      });
      if (!response.data?.success) {
        throw new Error(response.data?.message || t('连接失败'));
      }
      showSuccess(
        t('连接成功，延迟 {{latency}} ms', {
          latency: response.data.latency_ms || 0,
        }),
      );
    } catch (error) {
      showError(error instanceof Error ? error.message : t('连接失败'));
    } finally {
      setTesting(false);
    }
  }

  async function removeToken() {
    setLoading(true);
    try {
      const response = await API.put('/api/option/', {
        key: 'PromptAuditAPIKey',
        value: '',
      });
      if (!response.data?.success) {
        throw new Error(response.data?.message || 'option update failed');
      }
      showSuccess(t('令牌已移除'));
      props.refresh();
    } catch (error) {
      showError(error instanceof Error ? error.message : t('移除失败'));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    const currentInputs = { ...initialInputs };
    for (const key in props.options) {
      if (key in currentInputs) {
        currentInputs[key] = props.options[key];
      }
    }
    currentInputs.PromptAuditAPIKey = '';
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  const selectedScanners =
    inputs.PromptAuditScanners.split(',').filter(Boolean);
  const groupPolicies = parseGroupPolicies(inputs.PromptAuditGroupPolicies);
  const availableGroups = getAvailableGroups(
    props.options.GroupRatio,
    groupPolicies,
  );

  function updateGroupPolicies(nextPolicies) {
    setInputs((previous) => ({
      ...previous,
      PromptAuditGroupPolicies: serializeGroupPolicies(nextPolicies),
    }));
  }

  function updateGroupPolicy(index, values) {
    updateGroupPolicies(
      groupPolicies.map((policy, policyIndex) =>
        policyIndex === index ? { ...policy, ...values } : policy,
      ),
    );
  }

  function addGroupPolicy() {
    const selected = new Set(groupPolicies.map((policy) => policy.group));
    const group = availableGroups.find((candidate) => !selected.has(candidate));
    if (!group) return;
    updateGroupPolicies([
      ...groupPolicies,
      {
        group,
        enabled: true,
        failClosed: inputs.PromptAuditFailClosed,
        scanners: selectedScanners,
      },
    ]);
  }

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('Prompt 审核')}>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                field='PromptAuditEnabled'
                label={t('启用 Prompt 审核')}
                checkedText='｜'
                uncheckedText='〇'
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditEnabled: value,
                  }))
                }
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                field='PromptAuditFailClosed'
                label={t('审核后端不可用时拒绝请求')}
                checkedText='｜'
                uncheckedText='〇'
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditFailClosed: value,
                  }))
                }
              />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={12} lg={12} xl={12}>
              <Form.Input
                field='PromptAuditBaseURL'
                label={t('审核后端 Base URL')}
                placeholder='http://qwen3guard:11434'
                extraText={t(
                  '自动拼接 OpenAI-compatible chat completions 路径',
                )}
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditBaseURL: value,
                  }))
                }
              />
            </Col>
            <Col xs={24} sm={12} md={12} lg={12} xl={12}>
              <Form.Input
                field='PromptAuditModel'
                label={t('审核模型')}
                placeholder='sileader/qwen3guard:0.6b'
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditModel: value,
                  }))
                }
              />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={12} lg={12} xl={12}>
              <Form.Input
                field='PromptAuditAPIKey'
                label={t('API Token（可选）')}
                mode='password'
                placeholder={t('留空则保留现有令牌')}
                extraText={
                  inputs.PromptAuditKeyConfigured
                    ? t('已配置令牌；本地 QwenGuard 通常不需要')
                    : t('允许匿名访问的后端可留空')
                }
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditAPIKey: value,
                  }))
                }
              />
            </Col>
            <Col xs={24} sm={8} md={4} lg={4} xl={4}>
              <Form.InputNumber
                field='PromptAuditTimeoutMS'
                label={t('总超时（毫秒）')}
                min={1}
                max={120000}
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditTimeoutMS: value || 1,
                  }))
                }
              />
            </Col>
            <Col xs={24} sm={8} md={4} lg={4} xl={4}>
              <Form.InputNumber
                field='PromptAuditInputLimit'
                label={t('分块字符数')}
                min={1}
                max={65536}
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditInputLimit: value || 1,
                  }))
                }
              />
            </Col>
            <Col xs={24} sm={8} md={4} lg={4} xl={4}>
              <Form.InputNumber
                field='PromptAuditMaxConcurrency'
                label={t('最大并发')}
                min={1}
                max={128}
                onChange={(value) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditMaxConcurrency: value || 1,
                  }))
                }
              />
            </Col>
          </Row>
          <Row style={{ marginBottom: 18 }}>
            <Col span={24}>
              <div style={{ marginBottom: 8 }}>{t('阻断类别')}</div>
              <Checkbox.Group
                value={selectedScanners}
                onChange={(values) =>
                  setInputs((previous) => ({
                    ...previous,
                    PromptAuditScanners: values.join(','),
                  }))
                }
              >
                {scannerOptions.map(([id, label]) => (
                  <Checkbox
                    key={id}
                    value={id}
                    style={{ margin: '4px 16px 4px 0' }}
                  >
                    {t(label)}
                  </Checkbox>
                ))}
              </Checkbox.Group>
            </Col>
          </Row>
          <div
            style={{
              borderTop: '1px solid var(--semi-color-border)',
              paddingTop: 16,
              marginBottom: 18,
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                marginBottom: 12,
                flexWrap: 'wrap',
              }}
            >
              <Typography.Text strong>{t('分组审核策略')}</Typography.Text>
              <Button
                icon={<IconPlus />}
                theme='outline'
                onClick={addGroupPolicy}
                disabled={availableGroups.every((group) =>
                  groupPolicies.some((policy) => policy.group === group),
                )}
              >
                {t('添加分组策略')}
              </Button>
            </div>
            {groupPolicies.map((policy, index) => {
              const selectedByOthers = new Set(
                groupPolicies
                  .filter((_, policyIndex) => policyIndex !== index)
                  .map((item) => item.group),
              );
              const optionList = availableGroups
                .filter(
                  (group) =>
                    group === policy.group || !selectedByOthers.has(group),
                )
                .map((group) => ({ label: group, value: group }));
              return (
                <div
                  key={`${policy.group}-${index}`}
                  style={{
                    border: '1px solid var(--semi-color-border)',
                    borderRadius: 6,
                    padding: 16,
                    marginBottom: 12,
                  }}
                >
                  <Row gutter={16} type='flex' align='middle'>
                    <Col xs={24} sm={8} md={8} lg={8} xl={8}>
                      <Typography.Text size='small'>
                        {t('分组')}
                      </Typography.Text>
                      <Select
                        value={policy.group}
                        optionList={optionList}
                        style={{ width: '100%', marginTop: 6 }}
                        onChange={(group) =>
                          updateGroupPolicy(index, { group })
                        }
                      />
                    </Col>
                    <Col xs={12} sm={5} md={5} lg={5} xl={5}>
                      <Space>
                        <Typography.Text size='small'>
                          {t('审核')}
                        </Typography.Text>
                        <Switch
                          checked={policy.enabled}
                          onChange={(enabled) =>
                            updateGroupPolicy(index, { enabled })
                          }
                        />
                      </Space>
                    </Col>
                    <Col xs={12} sm={7} md={7} lg={7} xl={7}>
                      <Space>
                        <Typography.Text size='small'>
                          {t('失败时拒绝')}
                        </Typography.Text>
                        <Switch
                          checked={policy.failClosed}
                          onChange={(failClosed) =>
                            updateGroupPolicy(index, { failClosed })
                          }
                        />
                      </Space>
                    </Col>
                    <Col xs={24} sm={4} md={4} lg={4} xl={4}>
                      <Button
                        icon={<IconDelete />}
                        type='danger'
                        theme='borderless'
                        title={t('删除分组策略')}
                        aria-label={t('删除分组策略')}
                        onClick={() =>
                          updateGroupPolicies(
                            groupPolicies.filter(
                              (_, policyIndex) => policyIndex !== index,
                            ),
                          )
                        }
                      />
                    </Col>
                  </Row>
                  <div style={{ marginTop: 14 }}>
                    <Typography.Text size='small'>
                      {t('阻断类别')}
                    </Typography.Text>
                    <Checkbox.Group
                      value={policy.scanners}
                      style={{ display: 'block', marginTop: 6 }}
                      onChange={(scanners) =>
                        updateGroupPolicy(index, { scanners })
                      }
                    >
                      {scannerOptions.map(([id, label]) => (
                        <Checkbox
                          key={id}
                          value={id}
                          style={{ margin: '4px 16px 4px 0' }}
                        >
                          {t(label)}
                        </Checkbox>
                      ))}
                    </Checkbox.Group>
                  </div>
                </div>
              );
            })}
          </div>
          <Row>
            <Space wrap>
              <Button size='default' onClick={onSubmit}>
                {t('保存 Prompt 审核设置')}
              </Button>
              <Button
                size='default'
                theme='light'
                loading={testing}
                onClick={testConnection}
              >
                {t('测试连接')}
              </Button>
              {inputs.PromptAuditKeyConfigured ? (
                <Button
                  size='default'
                  theme='borderless'
                  type='danger'
                  onClick={removeToken}
                >
                  {t('移除令牌')}
                </Button>
              ) : null}
            </Space>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
