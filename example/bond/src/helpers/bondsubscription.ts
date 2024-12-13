import PaladinClient, {
  PaladinVerifier,
  PentePrivacyGroup,
  PentePrivateContract,
} from "@lfdecentralizedtrust-labs/paladin-sdk";
import bondSubscription from "../abis/BondSubscription.json";

const bondSubscriptionConstructor = bondSubscription.abi.find(
  (entry) => entry.type === "constructor"
);

export interface BondSubscriptionConstructorParams {
  bondAddress_: string;
  units_: string | number;
  custodian_: string;
  atomFactory_: string;
}

export interface PreparePaymentParams {
  to: string;
  encodedCall: string;
}

export interface PrepareBondParams {
  to: string;
  encodedCall: string;
}

export const newBondSubscription = async (
  pente: PentePrivacyGroup,
  from: PaladinVerifier,
  params: BondSubscriptionConstructorParams
) => {
  if (bondSubscriptionConstructor === undefined) {
    throw new Error("Bond subscription constructor not found");
  }
  const address = await pente.deploy(
    bondSubscription.abi,
    bondSubscription.bytecode,
    from,
    params
  );
  return address ? new BondSubscription(pente, address) : undefined;
};

export class BondSubscription extends PentePrivateContract<BondSubscriptionConstructorParams> {
  constructor(
    protected evm: PentePrivacyGroup,
    public readonly address: string
  ) {
    super(evm, bondSubscription.abi, address);
  }

  using(paladin: PaladinClient) {
    return new BondSubscription(this.evm.using(paladin), this.address);
  }

  preparePayment(from: PaladinVerifier, params: PreparePaymentParams) {
    return this.invoke(from, "preparePayment", params);
  }

  prepareBond(from: PaladinVerifier, params: PrepareBondParams) {
    return this.invoke(from, "prepareBond", params);
  }

  async distribute(from: PaladinVerifier) {
    return this.invoke(from, "distribute", {});
  }
}
