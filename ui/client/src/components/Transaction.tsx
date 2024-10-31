// Copyright © 2024 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import { Box, Button, Grid2, Typography } from "@mui/material";
import { IPaladinTransaction, ITransaction, ITransactionReceipt } from "../interfaces";
import { t } from "i18next";
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import { Hash } from "./Hash";
import { ViewDetailsDialog } from "../dialogs/ViewDetails";
import { useState } from "react";
import daysjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { EllapsedTime } from "./EllapsedTime";
import VisibilityIcon from '@mui/icons-material/VisibilityOutlined';
import { PaladinTransactionsDetailsDialog } from "../dialogs/TransactionDetails";

type Props = {
  transaction: ITransaction
  transactionReceipts?: ITransactionReceipt[]
  paladinTransactions?: IPaladinTransaction[]
}

daysjs.extend(relativeTime);

export const Transaction: React.FC<Props> = ({ transaction, paladinTransactions }) => {

  const [viewDetailsDialogOpen, setViewDetailsDialogOpen] = useState(false);

  return (
    <>
      <Box sx={{
        position: 'relative',
        backgroundColor: theme => theme.palette.background.paper,
        marginBottom: '20px', borderRadius: '4px'
      }}>
        {paladinTransactions && paladinTransactions.length > 0 &&
          <img src="/paladin-icon-light.svg" width="40" style={{ position: 'absolute', left: '20px', bottom: '0px' }} />
        }
        <Box sx={{ padding: '10px', paddingLeft: '20px', paddingRight: '20px', borderBottom: theme => `solid 1px ${theme.palette.divider}` }}>
          <Grid2 container direction="column" spacing={2}>
            <Grid2 container justifyContent="space-between">
              <Grid2>
                <Typography align="center" variant="h6" color="textPrimary">{transaction.blockNumber.toLocaleString()}</Typography>
                <Typography align="center" variant="body2" color="textSecondary">{t('block')}</Typography>
              </Grid2>
              <Grid2>
                <Typography align="center" variant="h6" color="textPrimary">{transaction.transactionIndex}</Typography>
                <Typography align="center" variant="body2" color="textSecondary">{t('transactionIndex')}</Typography>
              </Grid2>
              <Grid2>
                <Typography align="center" variant="h6" color="textPrimary">{transaction.nonce}</Typography>
                <Typography align="center" variant="body2" color="textSecondary">{t('nonce')}</Typography>
              </Grid2>
              {paladinTransactions && paladinTransactions.length > 1 &&
                <Grid2>
                  <Typography align="center" variant="h6" color="textPrimary">{t('atomicNumber', { number: paladinTransactions.length })}</Typography>
                  <Typography align="center" variant="body2" color="textSecondary">{t('type')}</Typography>
                </Grid2>}
              {paladinTransactions && paladinTransactions.length === 1 &&
                <Grid2>
                  <Typography align="center" variant="h6" color="textPrimary">{t(paladinTransactions[0].type)}</Typography>
                  <Typography align="center" variant="body2" color="textSecondary">{t('type')}</Typography>
                </Grid2>}
              <Grid2 sx={{ textAlign: 'center' }} alignContent="center">
                {transaction.result === 'success' ? <CheckCircleOutlineIcon color="primary" /> : <ErrorOutlineIcon color="error" />}
                <Typography align="center" variant="body2" color="textSecondary">{t('result')}</Typography>
              </Grid2>
            </Grid2>
          </Grid2>
        </Box>
        <Box sx={{ padding: '10px', paddingBottom: '20px' }}>
          <Grid2 container justifyContent="space-evenly" wrap="nowrap" spacing={2}>
            {transaction.contractAddress ?
              <>
                <Grid2 size={{ xs: 12, sm: 4 }}>
                  <Hash title={t('hash')} hash={transaction.hash} />
                </Grid2>
                <Grid2 size={{ xs: 12, sm: 4 }}>
                  <Hash title={t('from')} hash={transaction.from} />
                </Grid2>
                <Grid2 size={{ xs: 12, sm: 4 }}>
                  <Hash title={t('contract')} hash={transaction.contractAddress} />
                </Grid2>
              </>
              :
              <>
                <Grid2 size={{ xs: 12, sm: 6 }}>
                  <Hash title={t('hash')} hash={transaction.hash} />
                </Grid2>
                <Grid2 size={{ xs: 12, sm: 6 }}>
                  <Hash title={t('from')} hash={transaction.from} />
                </Grid2>
              </>
            }
          </Grid2>
        </Box>
        <Box sx={{ display: 'flex', padding: '4px', justifyContent: 'end' }}>
          <EllapsedTime timestamp={transaction.block.timestamp} />
          <Button sx={{ marginLeft: '20px', textTransform: 'none', fontWeight: '400' }} size="small" startIcon={<VisibilityIcon />}
            onClick={() => setViewDetailsDialogOpen(true)}>{t('viewDetails')}</Button>
        </Box>
      </Box>
      {paladinTransactions && paladinTransactions?.length > 0 ?
        <PaladinTransactionsDetailsDialog
          paladinTransactions={paladinTransactions}
          dialogOpen={viewDetailsDialogOpen}
          setDialogOpen={setViewDetailsDialogOpen}
        />
        :
        <ViewDetailsDialog
          title={t('transaction')}
          details={transaction}
          dialogOpen={viewDetailsDialogOpen}
          setDialogOpen={setViewDetailsDialogOpen}
        />}
    </>
  );

}