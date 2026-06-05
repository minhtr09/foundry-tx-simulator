import { z } from "zod";

const addressSchema = z.string().regex(/^0x[0-9a-fA-F]{40}$/);
const bytesSchema = z.string().regex(/^0x([0-9a-fA-F]{2})*$/);
const txHashSchema = z.string().regex(/^0x[0-9a-fA-F]{64}$/);
const uint256Schema = z.string().regex(/^(0x[0-9a-fA-F]+|[0-9]+)$/);

export const labelOverrideSchema = z.object({
  account: addressSchema,
  label: z.string().min(1)
});

export const erc20BalanceOverrideSchema = z.object({
  token: addressSchema,
  account: addressSchema,
  balance: uint256Schema
});

export const erc20ApprovalOverrideSchema = z.object({
  token: addressSchema,
  owner: addressSchema,
  spender: addressSchema,
  amount: uint256Schema
});

export const erc721ApprovalOverrideSchema = z.object({
  token: addressSchema,
  owner: addressSchema,
  spender: addressSchema,
  tokenId: uint256Schema
});

export const stateOverrideSchema = z.object({
  contractName: z.string().optional(),
  source: z.string()
});

export const compilerConfigSchema = z.object({
  use: z.string().optional(),
  offline: z.boolean().optional(),
  noAutoDetect: z.boolean().optional(),
  viaIR: z.boolean().optional(),
  useLiteralContent: z.boolean().optional(),
  noMetadata: z.boolean().optional(),
  evmVersion: z.string().optional(),
  optimize: z.boolean().optional(),
  optimizerRuns: z.number().int().min(0).max(4294967295).optional(),
  revertStrings: z.enum(["default", "strip", "debug", "verboseDebug"]).optional()
});

export const chainConfigSchema = z.object({
  chains: z.array(z.string()).default([]),
  explorerUrls: z.record(z.string(), z.string()).default({})
});

export const projectsResponseSchema = z.object({
  projects: z.array(z.string()).default([])
});

export const browseProjectResponseSchema = z.object({
  path: z.string().min(1)
});

export const errorResponseSchema = z.object({
  error: z.string()
});

export const simulateRequestSchema = z.object({
  chain: z.string().min(1),
  blockNumber: z.union([uint256Schema, z.literal("")]),
  projectPath: z.string().optional(),
  labelOverrides: z.array(labelOverrideSchema).optional(),
  erc20BalanceOverrides: z.array(erc20BalanceOverrideSchema).optional(),
  erc20ApprovalOverrides: z.array(erc20ApprovalOverrideSchema).optional(),
  erc721ApprovalOverrides: z.array(erc721ApprovalOverrideSchema).optional(),
  stateOverride: stateOverrideSchema.optional(),
  compiler: compilerConfigSchema.optional(),
  decodeInternal: z.boolean().default(false),
  sender: addressSchema,
  target: addressSchema,
  data: bytesSchema,
  value: uint256Schema.optional()
});

export const txRequestSchema = z.object({
  chain: z.string().min(1),
  txHash: txHashSchema,
  decodeInternal: z.boolean().default(false),
  quick: z.boolean().default(false)
});

export const requestKindSchema = z.enum(["simulation", "tx"]);
export const runRequestSchema = z.union([simulateRequestSchema, txRequestSchema]);

export const erc20TransferSchema = z.object({
  token: z.string(),
  from: z.string(),
  to: z.string(),
  amount: z.string(),
  normalizedAmount: z.string().optional(),
  symbol: z.string().optional(),
  logoUrl: z.string().optional()
});

export const tokenBalanceChangeSchema = z.object({
  user: z.string(),
  token: z.string(),
  symbol: z.string().optional(),
  logoUrl: z.string().optional(),
  rawAmount: z.string(),
  amount: z.string(),
  usdValue: z.number().optional()
});

export const userUSDChangeSchema = z.object({
  user: z.string(),
  usdValue: z.number()
});

export const balanceAnalysisSchema = z.object({
  changes: z.array(tokenBalanceChangeSchema).optional(),
  userTotals: z.array(userUSDChangeSchema).optional()
});

export const simulateResponseSchema = z.object({
  id: z.string(),
  success: z.boolean(),
  exitCode: z.number(),
  durationMillis: z.number(),
  trace: z.string(),
  erc20Transfers: z.array(erc20TransferSchema).optional(),
  balanceAnalysis: balanceAnalysisSchema.optional(),
  error: z.string().optional()
});

export const simulationRecordSchema = z
  .object({
    id: z.string(),
    kind: requestKindSchema.default("simulation"),
    request: runRequestSchema,
    response: simulateResponseSchema
  })
  .superRefine((record, context) => {
    const schema = record.kind === "simulation" ? simulateRequestSchema : txRequestSchema;
    if (!schema.safeParse(record.request).success) {
      context.addIssue({
        code: "custom",
        path: ["request"],
        message: `must match ${record.kind} request shape`
      });
    }
  });

export type LabelOverride = z.infer<typeof labelOverrideSchema>;
export type ERC20BalanceOverride = z.infer<typeof erc20BalanceOverrideSchema>;
export type ERC20ApprovalOverride = z.infer<typeof erc20ApprovalOverrideSchema>;
export type ERC721ApprovalOverride = z.infer<typeof erc721ApprovalOverrideSchema>;
export type StateOverride = z.infer<typeof stateOverrideSchema>;
export type CompilerConfig = z.infer<typeof compilerConfigSchema>;
export type ChainConfig = z.infer<typeof chainConfigSchema>;
export type ProjectsResponse = z.infer<typeof projectsResponseSchema>;
export type SimulateRequest = z.infer<typeof simulateRequestSchema>;
export type TxRequest = z.infer<typeof txRequestSchema>;
export type RequestKind = z.infer<typeof requestKindSchema>;
export type RunRequest = z.infer<typeof runRequestSchema>;
export type ERC20Transfer = z.infer<typeof erc20TransferSchema>;
export type TokenBalanceChange = z.infer<typeof tokenBalanceChangeSchema>;
export type UserUSDChange = z.infer<typeof userUSDChangeSchema>;
export type BalanceAnalysis = z.infer<typeof balanceAnalysisSchema>;
export type SimulateResponse = z.infer<typeof simulateResponseSchema>;
export type SimulationRecord = z.infer<typeof simulationRecordSchema>;
