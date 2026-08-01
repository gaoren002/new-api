/*
Copyright (C) 2023-2026 QuantumNous

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
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'

const SINGLE_STRING_EXAMPLE = `{
  "model": "qwen3guard",
  "input": "How do I reset my account password?"
}`

const STRING_ARRAY_EXAMPLE = `{
  "model": "qwen3guard",
  "input": [
    "First text to check",
    "Second text to check"
  ]
}`

const MESSAGES_EXAMPLE = `{
  "model": "qwen3guard",
  "input": [
    { "role": "system", "content": "You are a helpful assistant." },
    { "role": "user", "content": "Tell me how to pick a lock." },
    { "role": "assistant", "content": "Sorry, I can't help with that." }
  ]
}`

const RESPONSE_EXAMPLE = `{
  "id": "guardmod-4f2c1a7e-9b1d-4e5f-8a3c-2d6b7c8e9f01",
  "object": "guard.moderation",
  "created": 1755590400,
  "model": "qwen3guard",
  "results": [
    {
      "index": 0,
      "flagged": false,
      "risk_level": "safe",
      "refusal": false,
      "categories": {
        "violent": false,
        "non_violent_illegal_acts": false,
        "sexual_content_or_sexual_acts": false,
        "pii": false,
        "suicide_and_self_harm": false,
        "unethical_acts": false,
        "politically_sensitive_topics": false,
        "copyright_violation": false,
        "jailbreak": false
      },
      "category_scores": {
        "violent": 0,
        "non_violent_illegal_acts": 0,
        "sexual_content_or_sexual_acts": 0,
        "pii": 0,
        "suicide_and_self_harm": 0,
        "unethical_acts": 0,
        "politically_sensitive_topics": 0,
        "copyright_violation": 0,
        "jailbreak": 0
      }
    }
  ],
  "usage": {
    "prompt_tokens": 21,
    "completion_tokens": 5,
    "total_tokens": 26
  }
}`

const CURL_EXAMPLE = `curl https://your-newapi-host/v1/guard/moderations \\
  -H "Authorization: Bearer $NEWAPI_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "qwen3guard",
    "input": "How do I make a fake ID?"
  }'`

const PYTHON_EXAMPLE = `import requests

resp = requests.post(
    "https://your-newapi-host/v1/guard/moderations",
    headers={"Authorization": "Bearer <your token>"},
    json={
        "model": "qwen3guard",
        "input": [
            {"role": "user", "content": "Tell me how to pick a lock."},
            {"role": "assistant", "content": "Sorry, I can't help with that."},
        ],
    },
)
resp.raise_for_status()
for result in resp.json()["results"]:
    print(result["risk_level"], result["flagged"], result["refusal"])`

const NODE_EXAMPLE = `const resp = await fetch(
  'https://your-newapi-host/v1/guard/moderations',
  {
    method: 'POST',
    headers: {
      Authorization: \`Bearer \${process.env.NEWAPI_TOKEN}\`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      model: 'qwen3guard',
      input: ['First text to check', 'Second text to check'],
    }),
  }
)
const data = await resp.json()
for (const result of data.results) {
  console.log(result.index, result.risk_level, result.flagged)
}`

function CodeBlock({ code }: { code: string }) {
  return (
    <pre className='bg-muted overflow-x-auto rounded-lg p-4 text-sm'>
      <code>{code}</code>
    </pre>
  )
}

export function ModerationDocs() {
  const { t } = useTranslation()

  const responseFields: Array<{ field: string; description: string }> = [
    {
      field: 'flagged',
      description: t('True only when risk_level is unsafe.'),
    },
    {
      field: 'risk_level',
      description: t(
        'The three-tier Qwen3Guard verdict: safe, controversial, or unsafe.'
      ),
    },
    {
      field: 'refusal',
      description: t('True when the classified assistant turn is a refusal.'),
    },
    {
      field: 'categories',
      description: t(
        'Boolean flag per category; true means the category was detected.'
      ),
    },
    {
      field: 'category_scores',
      description: t(
        'Per-category score: 0 (safe), 0.5 (controversial), or 1.0 (unsafe).'
      ),
    },
    {
      field: 'usage',
      description: t(
        'Token usage of the guard call: prompt_tokens, completion_tokens, total_tokens.'
      ),
    },
  ]

  const categories: Array<{ id: string; label: string }> = [
    { id: 'violent', label: t('Violent') },
    { id: 'non_violent_illegal_acts', label: t('Non-violent illegal acts') },
    {
      id: 'sexual_content_or_sexual_acts',
      label: t('Sexual content'),
    },
    { id: 'pii', label: t('Personally identifiable information') },
    { id: 'suicide_and_self_harm', label: t('Suicide and self-harm') },
    { id: 'unethical_acts', label: t('Unethical acts') },
    {
      id: 'politically_sensitive_topics',
      label: t('Politically sensitive topics'),
    },
    { id: 'copyright_violation', label: t('Copyright violation') },
    { id: 'jailbreak', label: t('Jailbreak / prompt injection') },
  ]

  return (
    <PublicLayout>
      <div className='mx-auto max-w-4xl space-y-10 px-4 py-12'>
        <div className='space-y-3'>
          <h1 className='text-3xl font-semibold tracking-tight'>
            {t('Moderation API Documentation')}
          </h1>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'This platform provides a content moderation API powered by Qwen3Guard, a safety guardrail model that classifies prompts and responses for harmful content.'
            )}
          </p>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Each check returns a three-tier verdict — safe, controversial, or unsafe — along with per-category flags across nine risk categories.'
            )}
          </p>
        </div>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Endpoint')}
          </h2>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Send a POST request to the dedicated moderation endpoint. If the model field is omitted, it defaults to qwen3guard.'
            )}
          </p>
          <CodeBlock code='POST /v1/guard/moderations' />
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Alias: you can also call the standard OpenAI-compatible moderation path with model set to a qwen3guard-served model, so existing OpenAI SDK clients work without changes.'
            )}
          </p>
          <CodeBlock code='POST /v1/moderations' />
          <p className='text-muted-foreground leading-relaxed'>
            {t('Available models:')}{' '}
            <code className='bg-muted rounded px-1.5 py-0.5 text-sm'>
              qwen3guard
            </code>
            {', '}
            <code className='bg-muted rounded px-1.5 py-0.5 text-sm'>
              qwen3guard-gen-0.6b
            </code>
            {', '}
            <code className='bg-muted rounded px-1.5 py-0.5 text-sm'>
              qwen3guard-gen-4b
            </code>
            {', '}
            <code className='bg-muted rounded px-1.5 py-0.5 text-sm'>
              qwen3guard-gen-8b
            </code>
          </p>
          <h3 className='text-base font-semibold'>{t('Authentication')}</h3>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Pass your API token in the Authorization header. Token group, user group, auto group, and group ratio apply exactly like chat models.'
            )}
          </p>
          <CodeBlock code='Authorization: Bearer <your token>' />
        </section>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Request')}
          </h2>
          <p className='text-muted-foreground leading-relaxed'>
            {t('The input field accepts one of three shapes:')}
          </p>
          <h3 className='text-base font-semibold'>{t('Single string')}</h3>
          <p className='text-muted-foreground leading-relaxed'>
            {t('A single string performs one prompt check.')}
          </p>
          <CodeBlock code={SINGLE_STRING_EXAMPLE} />
          <h3 className='text-base font-semibold'>
            {t('Array of strings (up to 32)')}
          </h3>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'An array of up to 32 strings performs independent checks — one result per item, in the same order and index.'
            )}
          </p>
          <CodeBlock code={STRING_ARRAY_EXAMPLE} />
          <h3 className='text-base font-semibold'>
            {t('Conversation messages (up to 64)')}
          </h3>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'An array of up to 64 message objects with role and content performs a single conversation check that classifies the final turn with prior turns as context.'
            )}
          </p>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Roles must be system, user, or assistant; content must be a non-empty string; and the last message must be from user or assistant.'
            )}
          </p>
          <CodeBlock code={MESSAGES_EXAMPLE} />
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Mixing strings and message objects in one array is rejected with a 400 error.'
            )}
          </p>
        </section>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Response')}
          </h2>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'A successful request returns HTTP 200 with one result per input item:'
            )}
          </p>
          <CodeBlock code={RESPONSE_EXAMPLE} />
          <div className='overflow-x-auto'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='border-b text-left'>
                  <th className='py-2 pr-4 font-semibold'>{t('Field')}</th>
                  <th className='py-2 font-semibold'>{t('Description')}</th>
                </tr>
              </thead>
              <tbody>
                {responseFields.map((row) => (
                  <tr key={row.field} className='border-b'>
                    <td className='py-2 pr-4 align-top'>
                      <code className='bg-muted rounded px-1.5 py-0.5'>
                        {row.field}
                      </code>
                    </td>
                    <td className='text-muted-foreground py-2'>
                      {row.description}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Categories')}
          </h2>
          <p className='text-muted-foreground leading-relaxed'>
            {t('Every result reports the following nine risk categories:')}
          </p>
          <div className='overflow-x-auto'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='border-b text-left'>
                  <th className='py-2 pr-4 font-semibold'>
                    {t('Category ID')}
                  </th>
                  <th className='py-2 font-semibold'>{t('Label')}</th>
                </tr>
              </thead>
              <tbody>
                {categories.map((category) => (
                  <tr key={category.id} className='border-b'>
                    <td className='py-2 pr-4 align-top'>
                      <code className='bg-muted rounded px-1.5 py-0.5'>
                        {category.id}
                      </code>
                    </td>
                    <td className='text-muted-foreground py-2'>
                      {category.label}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Code Examples')}
          </h2>
          <h3 className='text-base font-semibold'>curl</h3>
          <CodeBlock code={CURL_EXAMPLE} />
          <h3 className='text-base font-semibold'>Python</h3>
          <CodeBlock code={PYTHON_EXAMPLE} />
          <h3 className='text-base font-semibold'>Node.js</h3>
          <CodeBlock code={NODE_EXAMPLE} />
        </section>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Billing & Groups')}
          </h2>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Moderation calls are billed per token: the prompt and completion tokens of the guard calls, multiplied by the model ratio and your group ratio.'
            )}
          </p>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Usage appears in the usage logs like any other model, and token groups and auto group selection work as usual.'
            )}
          </p>
        </section>

        <section className='space-y-3'>
          <h2 className='text-xl font-semibold tracking-tight'>
            {t('Errors')}
          </h2>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'A 400 error is returned for invalid input: empty input, more than 32 string items, more than 64 messages, mixed strings and objects, invalid roles, empty content, or a final message with the system role.'
            )}
          </p>
          <p className='text-muted-foreground leading-relaxed'>
            {t(
              'Upstream failures surface as standard relay errors and are automatically retried across available channels.'
            )}
          </p>
        </section>
      </div>
    </PublicLayout>
  )
}
