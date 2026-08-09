export type ModelDetailsSampleLanguage =
  | 'curl'
  | 'python'
  | 'typescript'
  | 'javascript'

type ResponsesCompactSampleContext = {
  baseUrl: string
  apiKeyEnv: string
  modelName: string
  endpointPath: string
}

export function buildResponsesCompactSample(
  lang: ModelDetailsSampleLanguage,
  ctx: ResponsesCompactSampleContext
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
