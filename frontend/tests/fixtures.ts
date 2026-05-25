import type { Page } from "@playwright/test";

export const apiURL = "http://127.0.0.1:8080";
export const explorerURL = "https://explorer.test";
export const token = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2";
export const owner = "0x0000000000000000000000000000000000000001";
export const spender = "0x0000000000000000000000000000000000000002";
export const recipient = "0x0000000000000000000000000000000000000003";
export const searchOnlyAccount = "0x0000000000000000000000000000000000000004";
export const longBytes = `0x${"a".repeat(64)}`;
export const shortBytes = "0xaaaaaaaa...aaaaaaaa";
export const helper = "0x1111111111111111111111111111111111111111";

export async function routeBaseEndpoints(page: Page) {
  await page.route(`${apiURL}/health`, async (route) => {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ok: true }) });
  });
  await page.route(`${apiURL}/chains`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        chains: ["mainnet"],
        explorerUrls: {
          mainnet: explorerURL
        }
      })
    });
  });
  await page.route(`${apiURL}/projects`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ projects: ["~/Kyber/ks-dex-aggregator-sc"] })
    });
  });
}

export function simulateResponse() {
  const eventCount = 50;
  const transfers = [
    ...Array.from({ length: eventCount }, () => ({
      token,
      from: owner,
      to: recipient,
      amount: "1000000000000000000",
      normalizedAmount: "1",
      symbol: "WETH",
      logoUrl: ""
    })),
    {
      token,
      from: owner,
      to: helper,
      amount: "1000000000000000000",
      normalizedAmount: "1",
      symbol: "WETH",
      logoUrl: ""
    },
    {
      token,
      from: helper,
      to: spender,
      amount: "1000000000000000000",
      normalizedAmount: "1",
      symbol: "WETH",
      logoUrl: ""
    },
    {
      token,
      from: spender,
      to: recipient,
      amount: "1000000000000000000",
      normalizedAmount: "1",
      symbol: "WETH",
      logoUrl: ""
    }
  ];

  return {
    id: "browser-test",
    success: true,
    exitCode: 0,
    durationMillis: 12,
    trace: forgeTraceFixture(eventCount),
    erc20Transfers: transfers,
    balanceAnalysis: {
      changes: [
        {
          user: owner,
          token,
          symbol: "WETH",
          rawAmount: "-1000000000000000000",
          amount: "-1",
          usdValue: -3500
        },
        {
          user: recipient,
          token,
          symbol: "WETH",
          rawAmount: "1000000000000000000",
          amount: "1",
          usdValue: 3500
        }
      ],
      userTotals: [
        {
          user: owner,
          usdValue: -3500
        },
        {
          user: recipient,
          usdValue: 3500
        }
      ]
    }
  };
}

function forgeTraceFixture(eventCount: number): string {
  const tracePayload = {
    logs: [],
    returns: {},
    success: true,
    raw_logs: [],
    traces: [
      [
        "Execution",
        {
          arena: [
            forgeCallNode({
              idx: 0,
              children: [1, 2],
              address: "0x9999999999999999999999999999999999999999",
              signature:
                "run((address,string)[],(address,address,uint256)[],(address,address,address,uint256)[],(address,address,address,uint256)[],bytes,address,address,bytes)",
              args: ["0x"],
              gasUsed: 1000,
              status: "Stop",
              ordering: [{ Call: 0 }, { Call: 1 }]
            }),
            forgeCallNode({
              idx: 1,
              parent: 0,
              address: token,
              label: "WETH9",
              signature: "approve(address,uint256)",
              args: [`Sender: [${spender}]`, "1000000000000000000"],
              gasUsed: 100,
              status: "Return"
            }),
            forgeCallNode({
              idx: 2,
              parent: 0,
              children: [3, 4, 5, 6, 7],
              address: token,
              label: "WETH9",
              kind: "DELEGATECALL",
              signature: "transferFrom(address,address,uint256)",
              data: "0x23b872dd",
              args: [`srcToken: WETH9: [${token}]`, owner, recipient, "1000000000000000000", `WETH: [${token}]`, `callTarget: [${helper}]`, longBytes],
              gasUsed: 400,
              status: "Return",
              logs: Array.from({ length: eventCount }, (_, index) => transferLog(index + 1)),
              ordering: [{ Call: 0 }, { Call: 1 }, { Call: 2 }, { Call: 3 }, { Call: 4 }, ...Array.from({ length: eventCount }, (_, index) => ({ Log: index }))]
            }),
            forgeCallNode({
              idx: 3,
              parent: 2,
              address: spender,
              label: "Sender",
              signature: "fallback()",
              gasUsed: 40,
              status: "Return"
            }),
            forgeCallNode({
              idx: 4,
              parent: 2,
              address: helper,
              kind: "STATICCALL",
              signature: "decode(bytes)",
              args: [longBytes],
              gasUsed: 60,
              status: "Revert",
              output: "helper decode failed"
            }),
            forgeCallNode({
              idx: 5,
              parent: 2,
              address: "0x2222222222222222222222222222222222222222",
              label: "MetaAggregationRouterV2",
              signature: "swap()",
              gasUsed: 70,
              value: "0xde0b6b3a7640000",
              status: "Return"
            }),
            forgeCallNode({
              idx: 6,
              parent: 2,
              address: searchOnlyAccount,
              label: "SearchOnlyAlias",
              signature: "inspect()",
              gasUsed: 75,
              status: "Return"
            }),
            forgeCallNode({
              idx: 7,
              parent: 2,
              address: "0x3333333333333333333333333333333333333333",
              label: "UnmappedToken",
              signature: "transfer(address,uint256)",
              args: [recipient, "1000000000000000000"],
              gasUsed: 80,
              status: "Return"
            })
          ]
        }
      ]
    ],
    gas_used: 1000,
    labeled_addresses: {},
    returned: "0x",
    address: null
  };

  return JSON.stringify({
    traces: tracePayload.traces,
    labeled_addresses: tracePayload.labeled_addresses
  });
}

function forgeCallNode(options: {
  address: string;
  args?: unknown[];
  children?: number[];
  data?: string;
  gasUsed: number;
  idx: number;
  kind?: string;
  label?: string;
  logs?: unknown[];
  ordering?: unknown[];
  output?: string;
  parent?: number;
  signature: string;
  status: string;
  value?: string;
}) {
  return {
    parent: options.parent ?? null,
    children: options.children ?? [],
    idx: options.idx,
    trace: {
      depth: options.parent === undefined ? 0 : options.parent === 0 ? 1 : 2,
      success: options.status !== "Revert",
      caller: owner,
      address: options.address,
      kind: options.kind ?? "CALL",
      value: options.value ?? "0x0",
      data: options.data ?? "0x",
      output: options.output ?? "0x",
      gas_used: options.gasUsed,
      gas_limit: 1000000,
      status: options.status,
      steps: [],
      decoded: {
        label: options.label ?? "",
        return_data: "",
        call_data: {
          signature: options.signature,
          args: options.args ?? []
        }
      }
    },
    logs: options.logs ?? [],
    ordering: options.ordering ?? []
  };
}

function transferLog(value: number) {
  return {
    raw_log: {
      topics: [
        "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
        `0x000000000000000000000000${owner.slice(2)}`,
        `0x000000000000000000000000${recipient.slice(2)}`
      ],
      data: "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000"
    },
    decoded: {
      name: "Transfer",
      params: [
        ["from", `WETHOwner: [${owner}]`],
        ["to", `TraceRecipient: [${recipient}]`],
        ["value", `${value}`]
      ]
    },
    position: 0,
    index: value
  };
}
