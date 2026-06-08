// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "forge-std/interfaces/IERC20.sol";
import "forge-std/interfaces/IERC721.sol";

import "./SimulateTxRunner.t.sol";

contract SimulateTxTest is SimulateTxRunnerTest {
  address internal constant WETH = 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2;
  address internal constant BAYC = 0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D;
  uint256 internal constant BAYC_TOKEN_ID = 1;
  uint256 internal constant WETH_AMOUNT = 1 ether;
  address internal constant STATE_OVERRIDE_WETH_OWNER = 0x0000000000000000000000000000000000000011;
  address internal constant STATE_OVERRIDE_WETH_SPENDER = 0x0000000000000000000000000000000000000012;
  address internal constant STATE_OVERRIDE_ETH_SENDER = 0x0000000000000000000000000000000000000013;
  uint256 internal constant ETH_VALUE = 1 ether;

  function setUp() public {
    string memory rpcUrl = vm.envOr("MAINNET_RPC_URL", string(""));
    if (bytes(rpcUrl).length == 0) {
      rpcUrl = vm.envOr("ETH_RPC_URL", string(""));
    }
    vm.skip(bytes(rpcUrl).length == 0, "MAINNET_RPC_URL or ETH_RPC_URL is required");

    vm.createSelectFork(rpcUrl);
  }

  function testOverrideWETHBalanceAndApprovalThenTransferFrom() public {
    address owner = makeAddr("weth owner");
    address spender = makeAddr("weth spender");
    address recipient = makeAddr("weth recipient");

    SimulateTxRunnerTest.LabelOverride[] memory labelOverrides = new SimulateTxRunnerTest.LabelOverride[](3);
    labelOverrides[0] = SimulateTxRunnerTest.LabelOverride({account: owner, label: "WETHOwner"});
    labelOverrides[1] = SimulateTxRunnerTest.LabelOverride({account: spender, label: "WETHSpender"});
    labelOverrides[2] = SimulateTxRunnerTest.LabelOverride({account: recipient, label: "WETHRecipient"});

    SimulateTxRunnerTest.ERC20BalanceOverride[] memory erc20BalanceOverrides =
      new SimulateTxRunnerTest.ERC20BalanceOverride[](1);
    erc20BalanceOverrides[0] =
      SimulateTxRunnerTest.ERC20BalanceOverride({token: WETH, account: owner, balance: WETH_AMOUNT});

    SimulateTxRunnerTest.ERC20ApprovalOverride[] memory erc20ApprovalOverrides =
      new SimulateTxRunnerTest.ERC20ApprovalOverride[](1);
    erc20ApprovalOverrides[0] =
      SimulateTxRunnerTest.ERC20ApprovalOverride({token: WETH, owner: owner, spender: spender, amount: WETH_AMOUNT});

    SimulateTxRunnerTest.ERC721ApprovalOverride[] memory erc721ApprovalOverrides =
      new SimulateTxRunnerTest.ERC721ApprovalOverride[](0);

    uint256 recipientBalanceBefore = IERC20(WETH).balanceOf(recipient);

    _simulate(
      SimulateTxRunnerTest.SimulateRequest({
        chain: "",
        blockNumber: 0,
        projectPath: "",
        labelOverrides: labelOverrides,
        erc20BalanceOverrides: erc20BalanceOverrides,
        erc20ApprovalOverrides: erc20ApprovalOverrides,
        erc721ApprovalOverrides: erc721ApprovalOverrides,
        stateOverrideBytecode: "",
        sender: spender,
        target: WETH,
        data: abi.encodeCall(IERC20.transferFrom, (owner, recipient, WETH_AMOUNT)),
        value: 0
      })
    );

    assertEq(IERC20(WETH).balanceOf(owner), 0);
    assertEq(IERC20(WETH).balanceOf(recipient), recipientBalanceBefore + WETH_AMOUNT);
    assertEq(IERC20(WETH).allowance(owner, spender), 0);
  }

  function testStateOverrideContractDealsWETHBalanceAndApprovalThenTransferFrom() public {
    address owner = STATE_OVERRIDE_WETH_OWNER;
    address spender = STATE_OVERRIDE_WETH_SPENDER;
    address recipient = makeAddr("state override weth recipient");

    SimulateTxRunnerTest.LabelOverride[] memory labelOverrides = new SimulateTxRunnerTest.LabelOverride[](0);
    SimulateTxRunnerTest.ERC20BalanceOverride[] memory erc20BalanceOverrides =
      new SimulateTxRunnerTest.ERC20BalanceOverride[](0);
    SimulateTxRunnerTest.ERC20ApprovalOverride[] memory erc20ApprovalOverrides =
      new SimulateTxRunnerTest.ERC20ApprovalOverride[](0);
    SimulateTxRunnerTest.ERC721ApprovalOverride[] memory erc721ApprovalOverrides =
      new SimulateTxRunnerTest.ERC721ApprovalOverride[](0);

    uint256 recipientBalanceBefore = IERC20(WETH).balanceOf(recipient);

    _simulate(
      SimulateTxRunnerTest.SimulateRequest({
        chain: "",
        blockNumber: 0,
        projectPath: "",
        labelOverrides: labelOverrides,
        erc20BalanceOverrides: erc20BalanceOverrides,
        erc20ApprovalOverrides: erc20ApprovalOverrides,
        erc721ApprovalOverrides: erc721ApprovalOverrides,
        stateOverrideBytecode: type(WETHStateOverride).creationCode,
        sender: spender,
        target: WETH,
        data: abi.encodeCall(IERC20.transferFrom, (owner, recipient, WETH_AMOUNT)),
        value: 0
      })
    );

    assertEq(IERC20(WETH).balanceOf(owner), 0);
    assertEq(IERC20(WETH).balanceOf(recipient), recipientBalanceBefore + WETH_AMOUNT);
    assertEq(IERC20(WETH).allowance(owner, spender), 0);
  }

  function testOverrideNFTApprovalThenTransferFrom() public {
    address owner = IERC721(BAYC).ownerOf(BAYC_TOKEN_ID);
    address spender = makeAddr("nft spender");
    address recipient = makeAddr("nft recipient");

    SimulateTxRunnerTest.LabelOverride[] memory labelOverrides = new SimulateTxRunnerTest.LabelOverride[](0);
    SimulateTxRunnerTest.ERC20BalanceOverride[] memory erc20BalanceOverrides =
      new SimulateTxRunnerTest.ERC20BalanceOverride[](0);
    SimulateTxRunnerTest.ERC20ApprovalOverride[] memory erc20ApprovalOverrides =
      new SimulateTxRunnerTest.ERC20ApprovalOverride[](0);
    SimulateTxRunnerTest.ERC721ApprovalOverride[] memory erc721ApprovalOverrides =
      new SimulateTxRunnerTest.ERC721ApprovalOverride[](1);
    erc721ApprovalOverrides[0] = SimulateTxRunnerTest.ERC721ApprovalOverride({
      token: BAYC, owner: owner, spender: spender, tokenId: BAYC_TOKEN_ID
    });

    _simulate(
      SimulateTxRunnerTest.SimulateRequest({
        chain: "",
        blockNumber: 0,
        projectPath: "",
        labelOverrides: labelOverrides,
        erc20BalanceOverrides: erc20BalanceOverrides,
        erc20ApprovalOverrides: erc20ApprovalOverrides,
        erc721ApprovalOverrides: erc721ApprovalOverrides,
        stateOverrideBytecode: "",
        sender: spender,
        target: BAYC,
        data: abi.encodeCall(IERC721.transferFrom, (owner, recipient, BAYC_TOKEN_ID)),
        value: 0
      })
    );

    assertEq(IERC721(BAYC).ownerOf(BAYC_TOKEN_ID), recipient);
  }

  function testStateOverrideDealsETHThenCallsWithValue() public {
    PayableReceiver receiver = new PayableReceiver();

    SimulateTxRunnerTest.LabelOverride[] memory labelOverrides = new SimulateTxRunnerTest.LabelOverride[](0);
    SimulateTxRunnerTest.ERC20BalanceOverride[] memory erc20BalanceOverrides =
      new SimulateTxRunnerTest.ERC20BalanceOverride[](0);
    SimulateTxRunnerTest.ERC20ApprovalOverride[] memory erc20ApprovalOverrides =
      new SimulateTxRunnerTest.ERC20ApprovalOverride[](0);
    SimulateTxRunnerTest.ERC721ApprovalOverride[] memory erc721ApprovalOverrides =
      new SimulateTxRunnerTest.ERC721ApprovalOverride[](0);

    _simulate(
      SimulateTxRunnerTest.SimulateRequest({
        chain: "",
        blockNumber: 0,
        projectPath: "",
        labelOverrides: labelOverrides,
        erc20BalanceOverrides: erc20BalanceOverrides,
        erc20ApprovalOverrides: erc20ApprovalOverrides,
        erc721ApprovalOverrides: erc721ApprovalOverrides,
        stateOverrideBytecode: type(ETHValueStateOverride).creationCode,
        sender: STATE_OVERRIDE_ETH_SENDER,
        target: address(receiver),
        data: abi.encodeCall(PayableReceiver.receiveValue, ()),
        value: ETH_VALUE
      })
    );

    assertEq(receiver.received(), ETH_VALUE);
    assertEq(address(receiver).balance, ETH_VALUE);
  }
}

contract WETHStateOverride is Test {
  address internal constant WETH = 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2;
  address internal constant OWNER = 0x0000000000000000000000000000000000000011;
  address internal constant SPENDER = 0x0000000000000000000000000000000000000012;
  uint256 internal constant AMOUNT = 1 ether;

  fallback() external {
    deal(WETH, OWNER, AMOUNT);
    vm.prank(OWNER);
    IERC20(WETH).approve(SPENDER, AMOUNT);
  }
}

contract ETHValueStateOverride is Test {
  address internal constant SENDER = 0x0000000000000000000000000000000000000013;
  uint256 internal constant AMOUNT = 1 ether;

  fallback() external {
    deal(SENDER, AMOUNT);
  }
}

contract PayableReceiver {
  uint256 public received;

  function receiveValue() external payable {
    received += msg.value;
  }

  function withdraw(address payable recipient) external {
    require(recipient != address(0), "recipient zero");
    recipient.transfer(address(this).balance);
  }
}
