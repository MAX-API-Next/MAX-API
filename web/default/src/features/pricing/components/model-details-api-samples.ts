export type ModelDetailsSampleLanguage =
  | 'curl'
  | 'python'
  | 'typescript'
  | 'javascript'

type ModelDetailsSampleContext = {
  baseUrl: string
  apiKeyEnv: string
  modelName: string
  endpointPath: string
}

export function buildResponsesCompactSample(
  lang: ModelDetailsSampleLanguage,
  ctx: ModelDetailsSampleContext
): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const input = 'Summarize the conversation context for the next request.'
  const bodyJson = JSON.stringify({ model: ctx.modelName, input }, null, 2)

  if (lang === 'curl') {
    return [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${bodyJson.replace(/\n/g, '\n     ')}'`,
    ].join('\n')
  }

  if (lang === 'python') {
    return [
      'import os',
      'import requests',
      '',
      `response = requests.post(`,
      `    "${url}",`,
      `    headers={`,
      `        "Authorization": f"Bearer {os.environ['${ctx.apiKeyEnv}']}",`,
      `        "Content-Type": "application/json",`,
      `    },`,
      `    json={`,
      `        "model": "${ctx.modelName}",`,
      `        "input": "${input}",`,
      `    },`,
      `)`,
      'response.raise_for_status()',
      'print(response.json())',
    ].join('\n')
  }

  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify(${bodyJson}),`,
    `})`,
    '',
    `if (!response.ok) throw new Error(\`HTTP \${response.status}\`)`,
    `console.log(await response.json())`,
  ].join('\n')
}

export function buildAlphaSearchSample(
  lang: ModelDetailsSampleLanguage,
  ctx: ModelDetailsSampleContext
): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const query = 'latest artificial intelligence news'
  const bodyJson = JSON.stringify(
    {
      model: ctx.modelName,
      input: [{ role: 'user', content: `Search for ${query}.` }],
      commands: { search_query: [{ q: query }] },
    },
    null,
    2
  )

  if (lang === 'curl') {
    return [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${bodyJson.replace(/\n/g, '\n     ')}'`,
    ].join('\n')
  }

  if (lang === 'python') {
    return [
      'import json',
      'import os',
      'import urllib.request',
      '',
      `payload = ${bodyJson}`,
      `request = urllib.request.Request(`,
      `    "${url}",`,
      `    data=json.dumps(payload).encode("utf-8"),`,
      `    headers={`,
      `        "Authorization": f"Bearer {os.environ['${ctx.apiKeyEnv}']}",`,
      `        "Content-Type": "application/json",`,
      `    },`,
      `    method="POST",`,
      `)`,
      '',
      'with urllib.request.urlopen(request) as response:',
      '    print(json.load(response))',
    ].join('\n')
  }

  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify(${bodyJson}),`,
    `})`,
    '',
    `const data = await response.json()`,
    `console.log(data)`,
  ].join('\n')
}
