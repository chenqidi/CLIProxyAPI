import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import {
  DEFAULT_USAGE_PRICE_SELECTED_MODEL,
  getWritableModelPriceKey,
  resolveModelPrice,
  type ModelPrice
} from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';

export interface PriceSettingsCardProps {
  modelNames: string[];
  selectedModel: string;
  modelPrices: Record<string, ModelPrice>;
  onSelectedModelChange: (model: string) => Promise<boolean>;
  onPricesChange: (
    prices: Record<string, ModelPrice>,
    options?: { action?: 'save' | 'delete' }
  ) => Promise<boolean>;
  saving?: boolean;
}

interface DraftPriceState {
  model: string;
  prompt: string;
  completion: string;
  cache: string;
}

const createDraftPriceState = (model: string, price: ModelPrice | null): DraftPriceState => ({
  model,
  prompt: price?.prompt.toString() ?? '',
  completion: price?.completion.toString() ?? '',
  cache: price?.cache.toString() ?? ''
});

const EMPTY_DRAFT_PRICE_STATE: DraftPriceState = {
  model: '',
  prompt: '',
  completion: '',
  cache: ''
};

export function PriceSettingsCard({
  modelNames,
  selectedModel,
  modelPrices,
  onSelectedModelChange,
  onPricesChange,
  saving = false
}: PriceSettingsCardProps) {
  const { t } = useTranslation();
  const selectedPrice = useMemo(
    () => resolveModelPrice(modelPrices, selectedModel),
    [modelPrices, selectedModel]
  );
  const [draftPrice, setDraftPrice] = useState<DraftPriceState>(EMPTY_DRAFT_PRICE_STATE);

  // Edit modal state
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editPrompt, setEditPrompt] = useState('');
  const [editCompletion, setEditCompletion] = useState('');
  const [editCache, setEditCache] = useState('');
  const usingDraftPrice = draftPrice.model === selectedModel;
  const promptPrice = usingDraftPrice ? draftPrice.prompt : selectedPrice?.prompt.toString() ?? '';
  const completionPrice = usingDraftPrice
    ? draftPrice.completion
    : selectedPrice?.completion.toString() ?? '';
  const cachePrice = usingDraftPrice ? draftPrice.cache : selectedPrice?.cache.toString() ?? '';

  const updateDraftPrice = (patch: Partial<Omit<DraftPriceState, 'model'>>) => {
    setDraftPrice((current) => {
      const base =
        current.model === selectedModel
          ? current
          : createDraftPriceState(selectedModel, selectedPrice);
      return {
        ...base,
        model: selectedModel,
        ...patch
      };
    });
  };

  const handleSavePrice = async () => {
    if (!selectedModel) return;
    const prompt = parseFloat(promptPrice) || 0;
    const completion = parseFloat(completionPrice) || 0;
    const cache = cachePrice.trim() === '' ? prompt : parseFloat(cachePrice) || 0;
    const targetModel = getWritableModelPriceKey(modelPrices, selectedModel);
    if (!targetModel) return;
    const newPrices = { ...modelPrices, [targetModel]: { prompt, completion, cache } };
    const saved = await onPricesChange(newPrices, { action: 'save' });
    if (!saved) return;
    setDraftPrice(createDraftPriceState(selectedModel, { prompt, completion, cache }));
  };

  const handleDeletePrice = async (model: string) => {
    const newPrices = { ...modelPrices };
    delete newPrices[model];
    const saved = await onPricesChange(newPrices, { action: 'delete' });
    if (!saved) return;
    if (model === selectedModel) {
      setDraftPrice(EMPTY_DRAFT_PRICE_STATE);
    }
  };

  const handleOpenEdit = (model: string) => {
    const price = modelPrices[model];
    setEditModel(model);
    setEditPrompt(price?.prompt?.toString() || '');
    setEditCompletion(price?.completion?.toString() || '');
    setEditCache(price?.cache?.toString() || '');
  };

  const handleSaveEdit = async () => {
    if (!editModel) return;
    const prompt = parseFloat(editPrompt) || 0;
    const completion = parseFloat(editCompletion) || 0;
    const cache = editCache.trim() === '' ? prompt : parseFloat(editCache) || 0;
    const newPrices = { ...modelPrices, [editModel]: { prompt, completion, cache } };
    const saved = await onPricesChange(newPrices, { action: 'save' });
    if (!saved) return;
    setEditModel(null);
  };

  const handleModelSelect = async (value: string) => {
    if (!value) return;
    setDraftPrice(EMPTY_DRAFT_PRICE_STATE);
    await onSelectedModelChange(value);
  };

  const options = useMemo(
    () =>
      Array.from(
        new Set([
          selectedModel,
          DEFAULT_USAGE_PRICE_SELECTED_MODEL,
          ...Object.keys(modelPrices),
          ...modelNames
        ])
      )
        .filter((name) => name.trim() !== '')
        .sort((left, right) => left.localeCompare(right))
        .map((name) => ({ value: name, label: name })),
    [modelNames, modelPrices, selectedModel]
  );

  return (
    <Card title={t('usage_stats.model_price_settings')}>
      <div className={styles.pricingSection}>
        {/* Price Form */}
        <div className={styles.priceForm}>
          <div className={styles.formRow}>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_name')}</label>
              <Select
                value={selectedModel}
                options={options}
                onChange={handleModelSelect}
                placeholder={t('usage_stats.model_price_select_placeholder')}
                disabled={saving}
              />
            </div>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
              <Input
                type="number"
                value={promptPrice}
                onChange={(e) => updateDraftPrice({ prompt: e.target.value })}
                placeholder="0.00"
                step="0.0001"
                disabled={saving}
              />
            </div>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
              <Input
                type="number"
                value={completionPrice}
                onChange={(e) => updateDraftPrice({ completion: e.target.value })}
                placeholder="0.00"
                step="0.0001"
                disabled={saving}
              />
            </div>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_cache')} ($/1M)</label>
              <Input
                type="number"
                value={cachePrice}
                onChange={(e) => updateDraftPrice({ cache: e.target.value })}
                placeholder="0.00"
                step="0.0001"
                disabled={saving}
              />
            </div>
            <Button variant="primary" onClick={() => void handleSavePrice()} disabled={!selectedModel} loading={saving}>
              {t('common.save')}
            </Button>
          </div>
        </div>

        {/* Saved Prices List */}
        <div className={styles.pricesList}>
          <h4 className={styles.pricesTitle}>{t('usage_stats.saved_prices')}</h4>
          {Object.keys(modelPrices).length > 0 ? (
            <div className={styles.pricesGrid}>
              {Object.entries(modelPrices).map(([model, price]) => (
                <div key={model} className={styles.priceItem}>
                  <div className={styles.priceInfo}>
                    <span className={styles.priceModel}>{model}</span>
                    <div className={styles.priceMeta}>
                      <span>
                        {t('usage_stats.model_price_prompt')}: ${price.prompt.toFixed(4)}/1M
                      </span>
                      <span>
                        {t('usage_stats.model_price_completion')}: ${price.completion.toFixed(4)}/1M
                      </span>
                      <span>
                        {t('usage_stats.model_price_cache')}: ${price.cache.toFixed(4)}/1M
                      </span>
                    </div>
                  </div>
                  <div className={styles.priceActions}>
                    <Button variant="secondary" size="sm" onClick={() => handleOpenEdit(model)} disabled={saving}>
                      {t('common.edit')}
                    </Button>
                    <Button variant="danger" size="sm" onClick={() => void handleDeletePrice(model)} disabled={saving}>
                      {t('common.delete')}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className={styles.hint}>{t('usage_stats.model_price_empty')}</div>
          )}
        </div>
      </div>

      {/* Edit Modal */}
      <Modal
        open={editModel !== null}
        title={editModel ?? ''}
        onClose={() => setEditModel(null)}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" onClick={() => setEditModel(null)} disabled={saving}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" onClick={() => void handleSaveEdit()} loading={saving}>
              {t('common.save')}
            </Button>
          </div>
        }
        width={420}
      >
        <div className={styles.editModalBody}>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
            <Input
              type="number"
              value={editPrompt}
              onChange={(e) => setEditPrompt(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={saving}
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
            <Input
              type="number"
              value={editCompletion}
              onChange={(e) => setEditCompletion(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={saving}
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_cache')} ($/1M)</label>
            <Input
              type="number"
              value={editCache}
              onChange={(e) => setEditCache(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={saving}
            />
          </div>
        </div>
      </Modal>
    </Card>
  );
}
