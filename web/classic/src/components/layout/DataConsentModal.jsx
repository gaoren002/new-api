import React, { useContext, useMemo, useState } from 'react';
import { Button, Modal, Typography } from '@douyinfe/semi-ui';
import { Database } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, setUserData } from '../../helpers';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';

const parseSettings = (setting) => {
  if (!setting) return {};
  if (typeof setting === 'object') return setting;
  try {
    return JSON.parse(setting);
  } catch {
    return {};
  }
};

const getConsentStatus = (settings, version) => {
  if (
    version &&
    settings.data_consent_version &&
    settings.data_consent_version !== version
  ) {
    return 'unset';
  }
  if (settings.data_consent_status === 'accepted') return 'accepted';
  if (settings.data_consent_status === 'declined') return 'declined';
  return 'unset';
};

const updateUserConsent = (user, status, version, updatedAt) => {
  const nextSettings = {
    ...parseSettings(user?.setting),
    data_consent_status: status,
    data_consent_version: version,
    data_consent_updated_at: updatedAt,
  };
  return {
    ...user,
    setting:
      typeof user?.setting === 'object'
        ? nextSettings
        : JSON.stringify(nextSettings),
  };
};

const DataConsentModal = () => {
  const { t } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState] = useContext(StatusContext);
  const [loading, setLoading] = useState(null);

  const status = statusState?.status || {};
  const user = userState?.user;
  const agreementVersion = status.data_consent_agreement_version || 'v1';
  const consentStatus = useMemo(
    () => getConsentStatus(parseSettings(user?.setting), agreementVersion),
    [user?.setting, agreementVersion],
  );
  const open =
    Boolean(status.data_consent_enabled) && Boolean(user) && consentStatus === 'unset';

  const submit = async (choice) => {
    if (!user) return;
    setLoading(choice);
    try {
      const res = await API.put('/api/user/data_consent', { status: choice });
      const { success, data, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      const nextUser = updateUserConsent(
        user,
        choice,
        data?.version || agreementVersion,
        data?.updated_at || Math.floor(Date.now() / 1000),
      );
      userDispatch({ type: 'login', payload: nextUser });
      setUserData(nextUser);
      showSuccess(t('数据授权偏好已保存'));
    } catch (error) {
      showError(t('设置保存失败'));
    } finally {
      setLoading(null);
    }
  };

  return (
    <Modal
      visible={open}
      closable={false}
      maskClosable={false}
      title={
        <div className='flex items-center gap-2'>
          <Database size={18} />
          {t('数据授权协议')}
        </div>
      }
      footer={
        <div className='flex justify-end gap-2'>
          <Button
            onClick={() => submit('declined')}
            loading={loading === 'declined'}
          >
            {t('拒绝')}
          </Button>
          <Button
            type='primary'
            onClick={() => submit('accepted')}
            loading={loading === 'accepted'}
          >
            {t('接受')}
          </Button>
        </div>
      }
    >
      <Typography.Paragraph>
        {t('授权 SuperAPI 收集你的对话数据，用于改善用户体验和改进国产模型。')}
      </Typography.Paragraph>
      <div className='rounded-lg border p-3 mb-2'>
        <div className='font-medium'>{t('接受授权')}</div>
        <div className='text-xs text-gray-500 mt-1'>
          {t('授权用户价格倍率')}：
          {status.data_consent_accepted_multiplier || 0.95}x
        </div>
      </div>
      <div className='rounded-lg border p-3'>
        <div className='font-medium'>{t('拒绝授权')}</div>
        <div className='text-xs text-gray-500 mt-1'>
          {t('不收集数据，价格倍率')}：
          {status.data_consent_declined_multiplier || 1.05}x
        </div>
      </div>
    </Modal>
  );
};

export default DataConsentModal;
