package santa

import (
	"context"
	"fmt"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

type SaveTracking struct {
	TrackingNumber string `json:"tracking_number"`
}

type Tracking struct {
	TrackingNumber string `json:"tracking_number"`
	Received       bool   `json:"received"`
}

type SetReceived struct {
	Received bool `json:"received"`
}

type Received struct {
	Received bool `json:"received"`
}

// SaveTracking lets the santa of a thread add or change the tracking number
// until the receiver confirms the gift.
func (s *Service) SaveTracking(ctx context.Context, actor Actor, key string, input SaveTracking) (Tracking, error) {
	var result Tracking
	err := s.write(ctx, func(tx *store.Store, n *notes) error {
		t, err := threadFor(ctx, tx, actor.ID, key)
		if err != nil {
			return err
		}
		if actor.ID != t.SenderID {
			return forbidden()
		}
		tracking, err := checkedText(input.TrackingNumber, MaxTracking)
		if err != nil {
			return err
		}
		delivery, err := tx.Delivery(ctx, t.Thread)
		if err != nil && !isNoRows(err) {
			return err
		}
		if delivery.Received {
			return conflict("Получатель уже подтвердил получение подарка")
		}
		result = Tracking{TrackingNumber: tracking}
		if delivery.TrackingNumber == tracking {
			return nil
		}
		if err := tx.SaveDelivery(ctx, &store.Delivery{Thread: t.Thread, TrackingNumber: tracking}, "tracking_number"); err != nil {
			return err
		}
		verb := "добавил"
		if delivery.TrackingNumber != "" {
			verb = "изменил"
		}
		n.add(t.ReceiverID, fmt.Sprintf("Ваш Тайный Санта %s трек-номер подарка в игре «%s»: %s", verb, t.GameName, tracking))
		return nil
	})
	return result, err
}

// SetReceived lets the receiver of a thread confirm or withdraw receiving the gift.
func (s *Service) SetReceived(ctx context.Context, actor Actor, key string, input SetReceived) (Received, error) {
	result := Received{Received: input.Received}
	err := s.write(ctx, func(tx *store.Store, n *notes) error {
		t, err := threadFor(ctx, tx, actor.ID, key)
		if err != nil {
			return err
		}
		if actor.ID != t.ReceiverID {
			return forbidden()
		}
		delivery, err := tx.Delivery(ctx, t.Thread)
		if err != nil && !isNoRows(err) {
			return err
		}
		if delivery.Received == input.Received {
			return nil
		}
		if err := tx.SaveDelivery(ctx, &store.Delivery{Thread: t.Thread, Received: input.Received}, "received"); err != nil {
			return err
		}
		receiver, err := tx.User(ctx, actor.ID)
		if err != nil {
			return err
		}
		text := receiver.Name + " подтвердил(а) получение подарка в игре «" + t.GameName + "»."
		if !input.Received {
			text = receiver.Name + " отменил(а) подтверждение получения подарка в игре «" + t.GameName + "»."
		}
		n.add(t.SenderID, text)
		return nil
	})
	if err != nil {
		return Received{}, err
	}
	return result, nil
}
