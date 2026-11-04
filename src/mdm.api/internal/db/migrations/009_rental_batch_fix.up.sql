-- rental_number is assigned by the application (all rentals of one request share a number).
--
-- This file used to also merge, with an UPDATE, every set of rentals with the same
-- borrower, purpose and borrow date into one rental_number, to repair data from before
-- batches shared a number. But the server runs every migration at each start, so that
-- UPDATE kept running: two separate requests of the same person with the same purpose
-- on the same day (say one rejected, one approved) were merged into one number at the
-- next restart, and the page showed the approved one as "rejected". The repair is long
-- done on existing databases and a new database has nothing to repair, so it is gone.

-- Remove auto-increment default so we control rental_number manually in application code
ALTER TABLE rentals ALTER COLUMN rental_number DROP DEFAULT;
