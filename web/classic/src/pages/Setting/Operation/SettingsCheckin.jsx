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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
  Col,
  Form,
  InputNumber,
  Row,
  Select,
  Spin,
  Table,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const DEFAULT_TIERS = [
  { min_used_cny: 0, min_cny: 0, max_cny: 0.2 },
  { min_used_cny: 100, min_cny: 0, max_cny: 1 },
  { min_used_cny: 300, min_cny: 0, max_cny: 2 },
  { min_used_cny: 1000, min_cny: 0, max_cny: 5 },
];

function parseTiers(raw) {
  if (!raw) return DEFAULT_TIERS;
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return DEFAULT_TIERS;
    return parsed
      .map((item) => ({
        min_used_cny: Number(item?.min_used_cny || 0),
        min_cny: Number(item?.min_cny || 0),
        max_cny: Number(item?.max_cny || 0),
      }))
      .filter(
        (item) =>
          Number.isFinite(item.min_used_cny) &&
          Number.isFinite(item.min_cny) &&
          Number.isFinite(item.max_cny),
      )
      .sort((a, b) => a.min_used_cny - b.min_used_cny);
  } catch {
    return DEFAULT_TIERS;
  }
}

function normalizeTiers(tiers) {
  return tiers
    .map((tier) => ({
      min_used_cny: Math.max(0, Math.round(Number(tier.min_used_cny) || 0)),
      min_cny: Math.max(0, Number(tier.min_cny) || 0),
      max_cny: Math.max(0, Number(tier.max_cny) || 0),
    }))
    .map((tier) =>
      tier.max_cny >= tier.min_cny
        ? tier
        : { ...tier, min_cny: tier.max_cny, max_cny: tier.min_cny },
    )
    .sort((a, b) => a.min_used_cny - b.min_used_cny);
}

function getDisplayUnitLabel(options = {}) {
  const displayType = options['general_setting.quota_display_type'] || 'USD';
  switch (displayType) {
    case 'CNY':
      return 'CNY';
    case 'TOKENS':
      return '额度';
    case 'CUSTOM':
      return (
        String(options['general_setting.custom_currency_symbol'] || '').trim() ||
        '自定义货币'
      );
    case 'USD':
    default:
      return 'USD';
  }
}

export default function SettingsCheckin(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const defaultInputs = useMemo(
    () => ({
      'checkin_setting.enabled': false,
      'checkin_setting.min_quota': 1000,
      'checkin_setting.max_quota': 10000,
      'checkin_setting.tiered': false,
      'checkin_setting.tiers': JSON.stringify(DEFAULT_TIERS),
      'checkin_setting.fallback_mode': 'legacy',
    }),
    [],
  );
  const [inputs, setInputs] = useState(defaultInputs);
  const [tiers, setTiers] = useState(DEFAULT_TIERS);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(defaultInputs);
  const displayUnitLabel = getDisplayUnitLabel(props.options);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function updateTier(index, field, value) {
    setTiers((current) => {
      const next = current.map((tier, currentIndex) =>
        currentIndex === index ? { ...tier, [field]: Number(value) || 0 } : tier,
      );
      setInputs((inputs) => ({
        ...inputs,
        'checkin_setting.tiers': JSON.stringify(normalizeTiers(next)),
      }));
      return next;
    });
  }

  function addTier() {
    setTiers((current) => {
      const lastTier = current[current.length - 1];
      const next = [
        ...current,
        {
          min_used_cny: lastTier ? lastTier.min_used_cny + 100 : 0,
          min_cny: 0,
          max_cny: lastTier ? lastTier.max_cny : 0.2,
        },
      ];
      setInputs((inputs) => ({
        ...inputs,
        'checkin_setting.tiers': JSON.stringify(normalizeTiers(next)),
      }));
      return next;
    });
  }

  function removeTier(index) {
    setTiers((current) => {
      if (current.length <= 1) return current;
      const next = current.filter((_, currentIndex) => currentIndex !== index);
      setInputs((inputs) => ({
        ...inputs,
        'checkin_setting.tiers': JSON.stringify(normalizeTiers(next)),
      }));
      return next;
    });
  }

  function onSubmit() {
    const normalizedInputs = {
      ...inputs,
      'checkin_setting.tiers': JSON.stringify(normalizeTiers(tiers)),
    };
    const updateArray = compareObjects(normalizedInputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) =>
      API.put('/api/option/', {
        key: item.key,
        value: String(normalizedInputs[item.key]),
      }),
    );
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = { ...defaultInputs };
    for (let key in props.options) {
      if (Object.keys(currentInputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    const parsedTiers = parseTiers(currentInputs['checkin_setting.tiers']);
    currentInputs['checkin_setting.tiers'] = JSON.stringify(
      normalizeTiers(parsedTiers),
    );
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    setTiers(parsedTiers);
    if (refForm.current) {
      refForm.current.setValues(currentInputs);
    }
  }, [props.options, defaultInputs]);

  const columns = [
    {
      title: `${t('累计使用门槛')} (${displayUnitLabel})`,
      dataIndex: 'min_used_cny',
      render: (value, record, index) => (
        <InputNumber
          value={value}
          min={0}
          step={1}
          precision={0}
          onChange={(v) => updateTier(index, 'min_used_cny', v)}
          style={{ width: '100%' }}
        />
      ),
    },
    {
      title: `${t('最小奖励')} (${displayUnitLabel})`,
      dataIndex: 'min_cny',
      render: (value, record, index) => (
        <InputNumber
          value={value}
          min={0}
          step={0.01}
          onChange={(v) => updateTier(index, 'min_cny', v)}
          style={{ width: '100%' }}
        />
      ),
    },
    {
      title: `${t('最大奖励')} (${displayUnitLabel})`,
      dataIndex: 'max_cny',
      render: (value, record, index) => (
        <InputNumber
          value={value}
          min={0}
          step={0.01}
          onChange={(v) => updateTier(index, 'max_cny', v)}
          style={{ width: '100%' }}
        />
      ),
    },
    {
      title: t('操作'),
      width: 64,
      render: (_, record, index) => (
        <Button
          icon={<IconDelete />}
          type='danger'
          theme='borderless'
          size='small'
          disabled={tiers.length <= 1}
          onClick={() => removeTier(index)}
        />
      ),
    },
  ];

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('签到设置')}>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              {t(
                '签到功能允许用户每日签到获取随机额度奖励。分层档位按当前额度显示单位配置。',
              )}
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'checkin_setting.enabled'}
                  label={t('启用签到功能')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('checkin_setting.enabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'checkin_setting.tiered'}
                  label={t('启用累计使用分层签到')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('checkin_setting.tiered')}
                  disabled={!inputs['checkin_setting.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <div style={{ marginBottom: 4 }}>{t('无匹配分层时')}</div>
                  <Select
                    value={inputs['checkin_setting.fallback_mode']}
                    onChange={handleFieldChange(
                      'checkin_setting.fallback_mode',
                    )}
                    disabled={
                      !inputs['checkin_setting.enabled'] ||
                      !inputs['checkin_setting.tiered']
                    }
                    style={{ width: '100%' }}
                  >
                    <Select.Option value='legacy'>
                      {t('使用旧版额度范围')}
                    </Select.Option>
                    <Select.Option value='none'>{t('发放 0')}</Select.Option>
                  </Select>
              </Col>
            </Row>

            {!inputs['checkin_setting.tiered'] && (
              <Row gutter={16}>
                <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                  <Form.InputNumber
                    field={'checkin_setting.min_quota'}
                    label={t('签到最小额度')}
                    placeholder={t('签到奖励的最小额度')}
                    onChange={handleFieldChange('checkin_setting.min_quota')}
                    min={0}
                    disabled={!inputs['checkin_setting.enabled']}
                  />
                </Col>
                <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                  <Form.InputNumber
                    field={'checkin_setting.max_quota'}
                    label={t('签到最大额度')}
                    placeholder={t('签到奖励的最大额度')}
                    onChange={handleFieldChange('checkin_setting.max_quota')}
                    min={0}
                    disabled={!inputs['checkin_setting.enabled']}
                  />
                </Col>
              </Row>
            )}

            {inputs['checkin_setting.tiered'] && (
              <>
                <Table
                  dataSource={tiers}
                  columns={columns}
                  pagination={false}
                  size='small'
                  rowKey={(record, index) => index}
                />
                <Button
                  icon={<IconPlus />}
                  onClick={addTier}
                  style={{ marginTop: 12, marginBottom: 16 }}
                  disabled={!inputs['checkin_setting.enabled']}
                >
                  {t('添加分层')}
                </Button>
              </>
            )}

            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存签到设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
