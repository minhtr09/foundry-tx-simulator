export type TraceNode = {
  raw: string;
  kind: string;
  gas?: number;
  parent?: number | null;
  target?: string;
  targetLabel?: string;
  function?: string;
  functionSignature?: string;
  selector?: string;
  arguments?: string;
  callType?: string;
  callValue?: string;
  resultType?: string;
  value?: string;
  children?: TraceNode[];
};

export type TraceAddressLabel = {
  address: string;
  label: string;
};
